// Package items is the server-side view of the client's Item_Prototype table
// (lobby rows only). The shop, Kitabe and flag systems all address items by the
// same prototype id, so purchase routing and inventory wiring key off Type.
//
// Type values are the Item_Prototype_LOL +0x08 enum as used by
// LgmShopItem::getOwnCount / IsItemLocked (libAndroid 3.6.5a).
package items

const (
	TypeRuneCurrency   = 0  // "N Runes" currency grants (595 = Rune)
	TypeConsumable     = 1  // potions, revival runes, tickets → ItemInfo inventory
	TypeHero           = 3  // hero unlock
	TypeEmblemCurrency = 4  // "N Emblems" currency grants (517 = Emblem)
	TypeSkin           = 6  // hero skin
	TypeGemCurrency    = 7  // 844 = gem
	TypeTablet         = 13 // Kitabe tablet → owned tablets vector
	TypeInscription    = 14 // Kitabe inscription → inscription map
	TypePack           = 15 // bundle; Pack lists the contents
	TypePole           = 16 // flag pole → HocPole map (owned iff count>0)
	TypePattern        = 17 // flag banner → HocFlag map (count; Unique = unlimited)
)

const (
	EmblemItemID = 517
	RuneItemID   = 595
	GemItemID    = 844
)

type PackEntry struct {
	ID    int
	Count int
}

type Item struct {
	ID     int
	Type   int
	Name   string
	Unique bool // patterns: "Unlimited use" (Item_Prototype_LOL::IsUniqueInPack)
	Wire   int  // tablets/inscriptions: proto+0x28, the PlayerInfo+0x358 in-match id
	Pack   []PackEntry
}

// MatchWireID maps a tablet/inscription id to the value the client's
// SyncPlayerInfoFromUserInfo writes into PlayerInfo+0x358.. (proto+0x28;
// FindAwakeItemIDFromPlayerInfo @0x107095c matches on it, raw ids never
// match). Unknown ids fall back to themselves.
func MatchWireID(id int) int {
	if it, ok := catalog[id]; ok && it.Wire != 0 {
		return it.Wire
	}
	return id
}

func Lookup(id int) (Item, bool) {
	it, ok := catalog[id]
	return it, ok
}

func TypeOf(id int) int {
	if it, ok := catalog[id]; ok {
		return it.Type
	}
	return -1
}

func Name(id int) string {
	if it, ok := catalog[id]; ok {
		return it.Name
	}
	return ""
}

func IsTablet(id int) bool      { return TypeOf(id) == TypeTablet }
func IsInscription(id int) bool { return TypeOf(id) == TypeInscription }
func IsPole(id int) bool        { return TypeOf(id) == TypePole }
func IsPattern(id int) bool     { return TypeOf(id) == TypePattern }
func IsConsumable(id int) bool  { return TypeOf(id) == TypeConsumable }

// PatternUnlimited reports whether a banner never runs out (seasonal/league).
func PatternUnlimited(id int) bool {
	it, ok := catalog[id]
	return ok && it.Type == TypePattern && it.Unique
}

// IDsOfType returns every lobby item id of the given type in ascending order.
func IDsOfType(typ int) []int {
	var out []int
	for id, it := range catalog {
		if it.Type == typ {
			out = append(out, id)
		}
	}
	sortInts(out)
	return out
}

// CurrencyAmount maps a currency grant item to (currency type, amount). The
// small "N Emblems"/"N Runes" reward rows encode the amount in their name; the
// pack contents use 517/595 with an explicit count instead, which callers
// multiply themselves.
func CurrencyAmount(id int) (typ int, amount int, ok bool) {
	it, found := catalog[id]
	if !found {
		return 0, 0, false
	}
	switch it.Type {
	case TypeEmblemCurrency, TypeRuneCurrency, TypeGemCurrency:
	default:
		return 0, 0, false
	}
	if id == EmblemItemID || id == RuneItemID || id == GemItemID {
		return it.Type, 1, true
	}
	n := leadingNumber(it.Name)
	if n <= 0 {
		return it.Type, 1, true
	}
	return it.Type, n, true
}

func leadingNumber(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// GrantableTabletIDs is the tablet set every account owned before inventory
// v1 (the former kitabe.TabletItemIDs list). 512 "Common Tablet" and 1043
// "Seal of Celerity" are deliberately absent, as before.
func GrantableTabletIDs() []int {
	ids := []int{453, 464, 465}
	for i := 467; i <= 479; i++ {
		ids = append(ids, i)
	}
	for i := 536; i <= 543; i++ {
		ids = append(ids, i)
	}
	for i := 556; i <= 561; i++ {
		ids = append(ids, i)
	}
	for i := 600; i <= 605; i++ {
		ids = append(ids, i)
	}
	for i := 649; i <= 654; i++ {
		ids = append(ids, i)
	}
	ids = append(ids, 673, 674)
	for i := 822; i <= 825; i++ {
		ids = append(ids, i)
	}
	ids = append(ids, 872, 873)
	return ids
}
