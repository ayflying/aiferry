package channel

import "testing"

func TestWeightedSelectKeepsZeroScoreProbe(t *testing.T) {
	rows := []credentialRow{{Id: 1}, {Id: 2}}
	states := map[uint64]comboHealth{1: {score: 0}, 2: {score: 0}}
	for i := 0; i < 20; i++ {
		got := weightedSelect(rows, states)
		if got.Id != 1 && got.Id != 2 {
			t.Fatal("零分冷却到期后必须保留试探候选")
		}
	}
}

func TestWeightedSelectSingleCredential(t *testing.T) {
	got := weightedSelect([]credentialRow{{Id: 7}}, map[uint64]comboHealth{7: {score: 0}})
	if got.Id != 7 {
		t.Fatal("唯一冷却到期凭证应可试探")
	}
}
