package talent

import (
	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/wire/msgpack"
)

func talentInfo(a, b, c int) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(3)...)
	out = append(out, msgpack.Int(int64(a))...)
	out = append(out, msgpack.Int(int64(b))...)
	out = append(out, msgpack.Int(int64(c))...)
	return out
}

func GroupInfo(g accounts.TalentGroupRec) []byte {
	pts := config.TalentPointsDefault
	echo := g.Echo
	if echo <= 0 {
		echo = pts
	}
	lim := g.Limit
	if lim <= 0 {
		lim = pts
	}
	// unlocked=True → wire bool=0 (RefreshPageInfo continues)
	wireBool := !g.Unlocked
	if !g.Unlocked {
		wireBool = true
	}
	talents := g.Talents
	var out []byte
	out = append(out, msgpack.FixArray(7)...)
	out = append(out, msgpack.Int(int64(echo))...)
	out = append(out, msgpack.FixArray(len(talents))...)
	for _, t := range talents {
		a, b, c := 0, 0, 0
		if len(t) > 0 {
			a = t[0]
		}
		if len(t) > 1 {
			b = t[1]
		}
		if len(t) > 2 {
			c = t[2]
		}
		out = append(out, talentInfo(a, b, c)...)
	}
	out = append(out, msgpack.Bool(wireBool)...)
	out = append(out, msgpack.Int(int64(g.F14))...)
	out = append(out, msgpack.Int(int64(g.F18))...)
	out = append(out, msgpack.Int(int64(lim))...)
	out = append(out, msgpack.Int(int64(g.F20))...)
	return out
}

func GroupMap(groups map[int]accounts.TalentGroupRec) []byte {
	keys := make([]int, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	var out []byte
	out = append(out, msgpack.FixMap(len(keys))...)
	for _, k := range keys {
		out = append(out, msgpack.Int(int64(k))...)
		out = append(out, GroupInfo(groups[k])...)
	}
	return out
}

func MapFromAccount(a *accounts.Account) []byte {
	if !config.ServerTalent {
		return msgpack.EmptyMap()
	}
	groups := map[int]accounts.TalentGroupRec{}
	if a != nil {
		groups = a.EnsureTalentPages()
	} else {
		for gid := 1; gid <= 7; gid++ {
			groups[gid] = accounts.TalentGroupRec{
				Echo: config.TalentPointsDefault, Unlocked: true,
				Limit: config.TalentPointsDefault, F18: config.TalentPointsDefault,
			}
		}
	}
	return GroupMap(groups)
}

func UnlockResponse(result, runeV, emblem int, groups map[int]accounts.TalentGroupRec) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(4)...)
	out = append(out, msgpack.Int(int64(result))...)
	out = append(out, msgpack.Int(int64(runeV))...)
	out = append(out, msgpack.Int(int64(emblem))...)
	out = append(out, GroupMap(groups)...)
	return out
}

func UnlockedTalentResponse(result int, name []byte, groupID int, g accounts.TalentGroupRec, pts int) []byte {
	var out []byte
	out = append(out, msgpack.FixArray(5)...)
	out = append(out, msgpack.Int(int64(result))...)
	out = append(out, msgpack.RawStr(name)...)
	out = append(out, msgpack.Int(int64(groupID))...)
	out = append(out, GroupInfo(g)...)
	out = append(out, msgpack.Int(int64(pts))...)
	return out
}
