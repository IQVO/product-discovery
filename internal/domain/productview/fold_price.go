package productview

import "regexp"

var currencyRx = regexp.MustCompile(`^[A-Z]{3}$`)

// PriceChanged is ItemPriceChanged (with the list price and currency of
// decision H2).
type PriceChanged struct {
	Currency            string
	ListPriceMinor      int64
	EffectivePriceMinor int64
	Version             int64
}

// ApplyPriceChanged sets the prices when the version is strictly newer than
// the one folded (the price-version guard). A changed price also revives a
// withdrawn one.
func (v View) ApplyPriceChanged(f PriceChanged) (View, Outcome, error) {
	if f.Version < 1 || !currencyRx.MatchString(f.Currency) || f.ListPriceMinor < 0 || f.EffectivePriceMinor < 0 {
		return v, Stale, invalid("price of %s: version %d currency %q", v.CatalogItemID, f.Version, f.Currency)
	}
	if f.Version <= v.PriceVersion {
		return v, Stale, nil
	}
	v.Price, v.PriceVersion = PriceActive, f.Version
	v.Currency, v.ListPriceMinor, v.EffectivePriceMinor = f.Currency, f.ListPriceMinor, f.EffectivePriceMinor
	return v, Applied, nil
}

// ApplyPriceWithdrawn withdraws the price unless a newer version was already
// folded. Withdrawing at the version already folded is the snapshot case and
// applies; withdrawing twice is stale.
func (v View) ApplyPriceWithdrawn(version int64) (View, Outcome, error) {
	if version < 1 {
		return v, Stale, invalid("price withdrawal of %s: version %d", v.CatalogItemID, version)
	}
	if v.Price == PriceWithdrawn || version < v.PriceVersion {
		return v, Stale, nil
	}
	v.Price, v.PriceVersion = PriceWithdrawn, version
	v.ListPriceMinor, v.EffectivePriceMinor = 0, 0
	return v, Applied, nil
}
