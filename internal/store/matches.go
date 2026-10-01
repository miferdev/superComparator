package store

type Match struct {
	ItemID      int64
	ItemName    string
	Quantity    int
	Chain       string
	URL         string
	SKU         string
	MatchedName string
	Score       float64
	Available   bool
}
