// restore-tablet-pages is a one-shot maintenance tool for the server-v0.1.10
// deploy. The v0.1.9 inventory migration bound each tablet copy to at most
// ONE page slot: a legacy account that had the same tablet on two pages
// (pages are alternative loadouts, so that is normal) lost the higher slot.
// This tool reads the pre-v0.1.9 backup next to the current store and
// re-creates every legacy slot the current store no longer has, bound to the
// copy that already serves that tablet id (or the first owned copy).
//
// Run it while hoc-server is STOPPED. A backup of the current store is
// written next to it before anything is changed.
//
//	restore-tablet-pages -accounts /var/lib/hoc-server/accounts.json \
//	    -legacy /opt/hoc-server/backups/<ts>/accounts.json.pre-v0.1.9 [-dry-run]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"
)

type slotRec struct {
	UID     int                    `json:"uid,omitempty"`
	ID      int                    `json:"id"`
	Sockets map[string][]int       `json:"sockets,omitempty"`
	Extra   map[string]interface{} `json:"-"`
}

type instance struct {
	UID     int              `json:"uid"`
	ID      int              `json:"id"`
	Sockets map[string][]int `json:"sockets,omitempty"`
	Awake   bool             `json:"awake,omitempty"`
}

func main() {
	cur := flag.String("accounts", "accounts.json", "current (inventory v2) accounts.json")
	legacy := flag.String("legacy", "", "pre-v0.1.9 backup accounts.json")
	dryRun := flag.Bool("dry-run", false, "report only, write nothing")
	flag.Parse()
	if *legacy == "" {
		fail("-legacy is required")
	}

	curRaw, err := os.ReadFile(*cur)
	if err != nil {
		fail("read current: %v", err)
	}
	legRaw, err := os.ReadFile(*legacy)
	if err != nil {
		fail("read legacy: %v", err)
	}
	var curDoc map[string]json.RawMessage
	if err := json.Unmarshal(curRaw, &curDoc); err != nil {
		fail("parse current: %v", err)
	}
	var curAccounts map[string]map[string]json.RawMessage
	if err := json.Unmarshal(curDoc["accounts"], &curAccounts); err != nil {
		fail("parse current accounts: %v", err)
	}
	var legDoc struct {
		Accounts map[string]struct {
			Tablets map[string]slotRec `json:"tablets"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(legRaw, &legDoc); err != nil {
		fail("parse legacy: %v", err)
	}

	names := make([]string, 0, len(curAccounts))
	for name := range curAccounts {
		names = append(names, name)
	}
	sort.Strings(names)
	accountsChanged, slotsRestored, skipped := 0, 0, 0
	for _, name := range names {
		leg, ok := legDoc.Accounts[name]
		if !ok || len(leg.Tablets) == 0 {
			continue
		}
		a := curAccounts[name]
		var slots map[string]slotRec
		if raw, ok := a["tablets"]; ok && len(raw) > 0 {
			if err := json.Unmarshal(raw, &slots); err != nil {
				fail("parse tablets of %s: %v", name, err)
			}
		}
		if slots == nil {
			slots = map[string]slotRec{}
		}
		var insts []instance
		if raw, ok := a["tablet_instances"]; ok {
			if err := json.Unmarshal(raw, &insts); err != nil {
				fail("parse tablet_instances of %s: %v", name, err)
			}
		}
		byUID := map[int]instance{}
		firstByID := map[int]instance{}
		for _, in := range insts {
			byUID[in.UID] = in
			if _, seen := firstByID[in.ID]; !seen {
				firstByID[in.ID] = in
			}
		}
		// uid already serving an id on some page → same copy on the restored page.
		servingUID := map[int]int{}
		for _, rec := range slots {
			if rec.UID > 0 {
				if _, seen := servingUID[rec.ID]; !seen {
					servingUID[rec.ID] = rec.UID
				}
			}
		}
		legKeys := make([]string, 0, len(leg.Tablets))
		for k := range leg.Tablets {
			legKeys = append(legKeys, k)
		}
		sort.Strings(legKeys)
		touched := false
		for _, key := range legKeys {
			legRec := leg.Tablets[key]
			if legRec.ID <= 0 {
				continue
			}
			if _, present := slots[key]; present {
				continue
			}
			var in instance
			if uid, ok := servingUID[legRec.ID]; ok {
				in = byUID[uid]
			} else if first, ok := firstByID[legRec.ID]; ok {
				in = first
			} else {
				fmt.Printf("%-28s %s id=%d: no owned copy, skipped\n", name, key, legRec.ID)
				skipped++
				continue
			}
			slots[key] = slotRec{UID: in.UID, ID: in.ID, Sockets: in.Sockets}
			servingUID[legRec.ID] = in.UID
			fmt.Printf("%-28s %s id=%d restored → uid=%d\n", name, key, legRec.ID, in.UID)
			slotsRestored++
			touched = true
		}
		if touched {
			enc, err := json.Marshal(slots)
			if err != nil {
				fail("encode tablets of %s: %v", name, err)
			}
			a["tablets"] = enc
			accountsChanged++
		}
	}
	fmt.Printf("%d slots restored on %d accounts, %d skipped (no owned copy)\n", slotsRestored, accountsChanged, skipped)
	if *dryRun || accountsChanged == 0 {
		return
	}

	backup := fmt.Sprintf("%s.%s.pre-restore-tablet-pages", *cur, time.Now().UTC().Format("20060102T150405Z"))
	if err := os.WriteFile(backup, curRaw, 0o600); err != nil {
		fail("backup: %v", err)
	}
	newAcc, err := json.Marshal(curAccounts)
	if err != nil {
		fail("encode accounts: %v", err)
	}
	curDoc["accounts"] = newAcc
	out, err := json.MarshalIndent(curDoc, "", "  ")
	if err != nil {
		fail("encode: %v", err)
	}
	tmp := *cur + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		fail("write: %v", err)
	}
	if err := os.Rename(tmp, *cur); err != nil {
		fail("rename: %v", err)
	}
	fmt.Printf("wrote %s (backup %s)\n", *cur, backup)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "restore-tablet-pages: "+format+"\n", args...)
	os.Exit(1)
}
