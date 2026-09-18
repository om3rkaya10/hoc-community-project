// clear-ascension is a one-shot maintenance tool: it rewrites accounts.json
// with every account's ascension flags cleared — legacy awake_tablets set to
// [] and, for inventory v2 records, every tablet_instances[].awake dropped.
//
// Run it while hoc-server is STOPPED (the server rewrites accounts.json on
// every mutation and would overwrite the edit). A backup copy of the input
// is written next to it before anything is changed.
//
//	clear-ascension -accounts /var/lib/hoc-server/accounts.json
//	clear-ascension -accounts accounts.json -dry-run
//
// Background: server versions up to v0.1.8 marked every worn tablet as
// ascended (TabletInfo[8]==2), so players ended up with tablets locked for
// editing that they never ascended. After the fix only a 0x4f request with
// the ascend flag sets it, so the stale flags are cleared once and players
// re-ascend the tablets they actually completed.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"
)

func main() {
	path := flag.String("accounts", "accounts.json", "path to accounts.json")
	dryRun := flag.Bool("dry-run", false, "report only, write nothing")
	flag.Parse()

	raw, err := os.ReadFile(*path)
	if err != nil {
		fail("read: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		fail("parse: %v", err)
	}
	accRaw, ok := doc["accounts"]
	if !ok {
		fail("no \"accounts\" object in %s", *path)
	}
	var accounts map[string]map[string]json.RawMessage
	if err := json.Unmarshal(accRaw, &accounts); err != nil {
		fail("parse accounts: %v", err)
	}

	empty := json.RawMessage("[]")
	names := make([]string, 0, len(accounts))
	for name := range accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	changed := 0
	for _, name := range names {
		a := accounts[name]
		touched := false
		cur, has := a["awake_tablets"]
		if !has || !bytes.Equal(bytes.TrimSpace(cur), empty) {
			fmt.Printf("%-40s awake_tablets %s -> []\n", name, describe(cur, has))
			a["awake_tablets"] = empty
			touched = true
		}
		// Inventory v2 keeps the flag per copy in tablet_instances[].awake.
		if raw, ok := a["tablet_instances"]; ok {
			var insts []map[string]json.RawMessage
			if err := json.Unmarshal(raw, &insts); err == nil {
				cleared := 0
				for _, inst := range insts {
					if v, ok := inst["awake"]; ok && bytes.Equal(bytes.TrimSpace(v), []byte("true")) {
						delete(inst, "awake")
						cleared++
					}
				}
				if cleared > 0 {
					enc, err := json.Marshal(insts)
					if err != nil {
						fail("encode tablet_instances of %s: %v", name, err)
					}
					a["tablet_instances"] = enc
					fmt.Printf("%-40s tablet_instances: %d ascended copies cleared\n", name, cleared)
					touched = true
				}
			}
		}
		if touched {
			changed++
		}
	}
	fmt.Printf("%d/%d accounts changed\n", changed, len(accounts))
	if *dryRun || changed == 0 {
		return
	}

	backup := fmt.Sprintf("%s.%s.pre-clear-ascension", *path, time.Now().UTC().Format("20060102T150405Z"))
	if err := os.WriteFile(backup, raw, 0o600); err != nil {
		fail("backup: %v", err)
	}
	newAcc, err := json.Marshal(accounts)
	if err != nil {
		fail("encode accounts: %v", err)
	}
	doc["accounts"] = newAcc
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fail("encode: %v", err)
	}
	tmp := *path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		fail("write: %v", err)
	}
	if err := os.Rename(tmp, *path); err != nil {
		fail("rename: %v", err)
	}
	fmt.Printf("wrote %s (backup %s)\n", *path, backup)
}

func describe(v json.RawMessage, has bool) string {
	if !has {
		return "<absent: legacy auto-ascend>"
	}
	return string(v)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "clear-ascension: "+format+"\n", args...)
	os.Exit(1)
}
