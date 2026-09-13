// Package matchpi derives the LoadMap PlayerInfo extras (awake tablets,
// talents, banner) from an account. The client fills these from its own
// UserInfo, but LoadMap's PlayerInfoDecode replaces the whole PlayerInfo with
// the wire, so an empty wire means no in-match passives — the same class of
// bug as the Python oracle's ints[29] mistake (RE_tablet_inmatch_apply).
package matchpi

import (
	"sort"

	"hoc-server/internal/accounts"
	"hoc-server/internal/domain/items"
	wiregs "hoc-server/internal/wire/gs"
)

const (
	defaultPole    = 562 // UserInfo::GetCurrentPole fallback (Rickety Pole)
	defaultPattern = 572 // UserInfo::GetCurrentPattern fallback (Artisan Banner - Gameloft)
	awakeSlots     = 20  // PlayerInfo+0x358..+0x3f0
)

// AwakeWire lists the awake equipped tablets and their socketed inscriptions
// as proto+0x28 wire ids, in (page, slot, socket) order, capped to the 20
// PlayerInfo slots. FindAwakeItemIDFromPlayerInfo needs >= 5 non-zero entries.
func AwakeWire(a *accounts.Account) []int32 {
	if a == nil {
		return nil
	}
	equipped := a.EquippedTablets()
	awake := a.AwakeTablets()
	keys := make([][2]int, 0, len(equipped))
	for k, e := range equipped {
		if e.ID > 0 && awake[e.ID] {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i][0] < keys[j][0] || (keys[i][0] == keys[j][0] && keys[i][1] < keys[j][1])
	})
	var out []int32
	for _, k := range keys {
		e := equipped[k]
		out = append(out, int32(items.MatchWireID(e.ID)))
		socks := make([]int, 0, len(e.Sockets))
		for si := range e.Sockets {
			socks = append(socks, si)
		}
		sort.Ints(socks)
		for _, si := range socks {
			if id := e.Sockets[si][0]; id > 0 {
				out = append(out, int32(items.MatchWireID(id)))
			}
		}
	}
	if len(out) > awakeSlots {
		out = out[:awakeSlots]
	}
	return out
}

// Talents picks the page with the most spent ranks (the oracle's rule; the
// client does not tell the server which page is active).
func Talents(a *accounts.Account) (page int32, pairs [][2]int32) {
	if a == nil {
		return 0, nil
	}
	best := -1
	for gid, g := range a.EnsureTalentPages() {
		var cur [][2]int32
		spent := 0
		for _, row := range g.Talents {
			if len(row) < 2 || row[0] <= 0 || row[1] <= 0 {
				continue
			}
			cur = append(cur, [2]int32{int32(row[0]), int32(row[1])})
			spent += row[1]
		}
		if spent > best || (spent == best && int32(gid) < page) {
			best, page, pairs = spent, int32(gid), cur
		}
	}
	if page == 0 {
		page = 1
	}
	return page, pairs
}

// Banner mirrors DlgMatchSettingViewBase::SyncPlayerBanners @0x1158574:
// GetCurrentPole / GetCurrentPattern (with the client defaults) and
// GetPatternCount(pattern).
func Banner(a *accounts.Account) (pole, pattern, uses int32) {
	p, pt, _ := a.FlagSelection()
	if p == 0 {
		p = defaultPole
	}
	if pt == 0 {
		pt = defaultPattern
	}
	return int32(p), int32(pt), int32(a.PatternCounts()[pt])
}

func Extra(a *accounts.Account) *wiregs.PIExtra {
	if a == nil {
		return nil
	}
	page, pairs := Talents(a)
	pole, pattern, uses := Banner(a)
	return &wiregs.PIExtra{
		AwakeWire: AwakeWire(a), TalentPage: page, Talents: pairs,
		Pole: pole, Pattern: pattern, PatternUses: uses,
	}
}
