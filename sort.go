// Copyright 2013-2014 Canonical Ltd.

package main


type tarballSlice []*Tarball

func (tbs tarballSlice) Len() int      { return len(tbs) }
func (tbs tarballSlice) Swap(i, j int) { tbs[i], tbs[j] = tbs[j], tbs[i] }

func (tbs tarballSlice) Less(i, j int) bool {
	a, b := tbs[i].Version, tbs[j].Version
	ai, bi := 0, 0
	for ai < len(a) && bi < len(b) {
		aIsDigit := a[ai] >= '0' && a[ai] <= '9'
		bIsDigit := b[bi] >= '0' && b[bi] <= '9'
		if aIsDigit != bIsDigit {
			return aIsDigit
		}
		if aIsDigit {
			av, bv := 0, 0
			for ai < len(a) && a[ai] >= '0' && a[ai] <= '9' {
				av *= 10
				av += int(a[ai] - '0')
				ai++
			}
			for bi < len(b) && b[bi] >= '0' && b[bi] <= '9' {
				bv *= 10
				bv += int(b[bi] - '0')
				bi++
			}
			if av != bv {
				return av > bv
			}
		} else if a[ai] == '.' && b[bi] == '.' {
			ai++
			bi++
		} else if a[ai] == '.' || b[bi] == '.' {
			return a[ai] == '.'
		} else {
			amark, bmark := ai, bi
			for ai < len(a) && a[ai] != '.' && (a[ai] < '0' || a[ai] > '9') {
				ai++
			}
			for bi < len(b) && b[bi] != '.' && (b[bi] < '0' || b[bi] > '9') {
				bi++
			}
			as := a[amark:ai]
			bs := b[bmark:bi]
			for _, s := range []string{"rc", "beta"} {
				if (as == s) != (bs == s) {
					return as == s
				}
			}
			if len(as) == 0 || len(bs) == 0 {
				return len(as) > len(bs)
			}
			if as != bs {
				return as > bs
			}
		}
	}
	if ai < len(a) && (a[ai] == '.' || a[ai] >= '0' && a[ai] <= '9') {
		return true
	}
	if bi < len(b) && b[bi] != '.' && (b[bi] < '0' || b[bi] >= '9') {
		return true
	}
	return false
}
