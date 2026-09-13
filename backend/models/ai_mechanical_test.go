package models

import "testing"

func card(suit, value string) Card {
	return Card{Suit: suit, Value: value, Type: "normal"}
}

func joker(value string) Card {
	return Card{Suit: "joker", Value: value, Type: "joker"}
}

func tableWithLead(trumpSuit, trumpRank string, leadCards []Card) *GameTable {
	t := &GameTable{
		TrumpSuit:   trumpSuit,
		TrumpRank:   trumpRank,
		TrickLeader: 5,
		PlayerHands: map[int]*PlayerHand{},
	}
	for _, c := range leadCards {
		t.CurrentTrick = append(t.CurrentTrick, PlayedCard{Card: c, Seat: 5, IsLead: true})
	}
	return t
}

func names(hand []Card, idxs []int) []string {
	out := make([]string, 0, len(idxs))
	for _, i := range idxs {
		out = append(out, hand[i].Suit+hand[i].Value)
	}
	return out
}

// 领出时必须出最小的一张单牌（牌型从小到大：单张 < 对子 < 三张 < 拖拉机）
func TestMechanicalLeadPlaysLowestSingle(t *testing.T) {
	hand := []Card{
		card("spades", "K"),
		card("diamonds", "9"),
		card("diamonds", "9"),
		card("clubs", "4"),
		joker("big"),
	}
	table := &GameTable{TrumpSuit: "hearts", TrumpRank: "2"}

	got := mechanicalPlay(hand, table)
	if len(got) != 1 || hand[got[0]].Suit != "clubs" || hand[got[0]].Value != "4" {
		t.Fatalf("领出应为最小单张 clubs4，实际 %v", names(hand, got))
	}
}

// 同等级时按「黑红花白」取小：黑桃 > 红桃 > 梅花 > 方块
func TestMechanicalSuitOrderBlackRedCloverWhite(t *testing.T) {
	hand := []Card{
		card("spades", "7"),
		card("hearts", "7"),
		card("clubs", "7"),
		card("diamonds", "7"),
	}
	table := &GameTable{TrumpSuit: "hearts", TrumpRank: "2"}

	got := mechanicalPlay(hand, table)
	if len(got) != 1 || hand[got[0]].Suit != "diamonds" {
		t.Fatalf("同点数应取方块，实际 %v", names(hand, got))
	}
}

// 垫牌时先出副牌、保住主牌
func TestMechanicalDiscardKeepsTrump(t *testing.T) {
	trumpSuit, trumpRank := "hearts", "2"
	lead := []Card{card("spades", "9")}
	table := tableWithLead(trumpSuit, trumpRank, lead)

	hand := []Card{
		card("hearts", "3"), // 主牌
		card("clubs", "A"),  // 副牌，点数比主牌大但等级低
	}
	table.PlayerHands[1] = &PlayerHand{Cards: hand, SeatNumber: 1}

	got := mechanicalPlay(hand, table)
	if len(got) != 1 || hand[got[0]].Suit != "clubs" {
		t.Fatalf("没有领出花色时应先垫副牌，实际 %v", names(hand, got))
	}
}

// 跟对子时出同花色里最小的一对
func TestMechanicalFollowPairPicksLowestPair(t *testing.T) {
	trumpSuit, trumpRank := "hearts", "2"
	lead := []Card{card("spades", "K"), card("spades", "K")}
	table := tableWithLead(trumpSuit, trumpRank, lead)

	hand := []Card{
		card("spades", "Q"),
		card("spades", "Q"),
		card("spades", "5"),
		card("spades", "5"),
		card("spades", "8"),
	}
	table.PlayerHands[1] = &PlayerHand{Cards: hand, SeatNumber: 1}

	got := mechanicalPlay(hand, table)
	if len(got) != 2 {
		t.Fatalf("应出 2 张，实际 %v", names(hand, got))
	}
	for _, i := range got {
		if hand[i].Value != "5" {
			t.Fatalf("应出最小的一对 spades5，实际 %v", names(hand, got))
		}
	}
}

// 领出主牌对子时，手里只有「同点数不同花色」的两张级牌，
// 按规则那不是对子，不能因此判定必须跟对子。
func TestMechanicalFollowTrumpPairWithSplitRankCards(t *testing.T) {
	trumpSuit, trumpRank := "clubs", "2"
	// 领出：梅花7 对子（梅花是主花色）
	lead := []Card{card("clubs", "7"), card("clubs", "7")}
	table := tableWithLead(trumpSuit, trumpRank, lead)

	// 手里的主牌：黑桃2、方片2（两张副级牌，花色不同，不构成对子）+ 梅花3
	hand := []Card{
		card("spades", "2"),
		card("diamonds", "2"),
		card("clubs", "3"),
		card("hearts", "9"),
	}
	table.PlayerHands[1] = &PlayerHand{Cards: hand, SeatNumber: 1}

	got := mechanicalPlay(hand, table)
	cards := pickCards(hand, got)
	if err := validateCardPlay(cards, table, table.PlayerHands[1]); err != nil {
		t.Fatalf("机械式出牌被判非法: %v (出的是 %v)", err, names(hand, got))
	}
}

// 同点数不同花色的两张牌不构成对子（RULE.md 5.2）
func TestSplitSuitSameValueIsNotPair(t *testing.T) {
	trumpSuit, trumpRank := "clubs", "2"
	lead := []Card{card("clubs", "7"), card("clubs", "7")}
	table := tableWithLead(trumpSuit, trumpRank, lead)

	hand := []Card{
		card("spades", "2"),
		card("diamonds", "2"),
		card("clubs", "3"),
		card("hearts", "9"),
	}
	ph := &PlayerHand{Cards: hand, SeatNumber: 1}
	table.PlayerHands[1] = ph

	// 出两张主牌散牌应当被接受，因为手里根本没有真正的对子
	play := []Card{card("spades", "2"), card("diamonds", "2")}
	if err := validateCardPlay(play, table, ph); err != nil {
		t.Fatalf("黑桃2+方片2 不是对子，不该被要求跟对子: %v", err)
	}
}

// RULE.md 5.3 规则4 / 5.4：没有领出花色时垫其他副牌，无法毙牌，领出方获胜。
// 「所有副牌都一样」——方块不能因为点数大就压过黑桃。
func TestDiscardedOffSuitCannotWinTrick(t *testing.T) {
	trumpSuit, trumpRank := "hearts", "2"
	trick := []PlayedCard{
		{Card: card("spades", "9"), Seat: 1, IsLead: true},
		{Card: card("diamonds", "10"), Seat: 2},
	}
	if w := determineTrickWinner(trick, trumpSuit, trumpRank); w != 1 {
		t.Fatalf("领出黑桃9、垫方片10，应由领出方(座位1)获胜，实际座位%d", w)
	}
}

// RULE.md 5.5：级牌属于主牌，可以毙副牌。
func TestRankCardIsTrumpAndBeatsPlainSuit(t *testing.T) {
	trumpSuit, trumpRank := "hearts", "2"
	trick := []PlayedCard{
		{Card: card("spades", "9"), Seat: 1, IsLead: true},
		{Card: card("spades", "2"), Seat: 2}, // 黑桃2 是副级牌，属于主牌
	}
	if w := determineTrickWinner(trick, trumpSuit, trumpRank); w != 2 {
		t.Fatalf("黑桃2 是级牌(主牌)，应毙掉黑桃9，实际座位%d", w)
	}
}

// 同一门副牌内部仍按点数比大小
func TestSameSuitStillComparesByValue(t *testing.T) {
	trumpSuit, trumpRank := "hearts", "2"
	trick := []PlayedCard{
		{Card: card("spades", "9"), Seat: 1, IsLead: true},
		{Card: card("spades", "K"), Seat: 2},
	}
	if w := determineTrickWinner(trick, trumpSuit, trumpRank); w != 2 {
		t.Fatalf("同为黑桃，K 应大于 9，实际座位%d", w)
	}
}

// 副级牌(700档)属于主牌，应大于主花色的小牌(600档)
func TestOffSuitRankCardBeatsLowTrumpSuitCard(t *testing.T) {
	trumpSuit, trumpRank := "hearts", "2"
	trick := []PlayedCard{
		{Card: card("spades", "9"), Seat: 1, IsLead: true},
		{Card: card("hearts", "3"), Seat: 2}, // 主花色3
		{Card: card("spades", "2"), Seat: 3}, // 副级牌，等级更高
	}
	if w := determineTrickWinner(trick, trumpSuit, trumpRank); w != 3 {
		t.Fatalf("副级牌黑桃2 应大于主花色红桃3，实际座位%d", w)
	}
}

// 王毙一切
func TestJokerBeatsEverything(t *testing.T) {
	trumpSuit, trumpRank := "hearts", "2"
	trick := []PlayedCard{
		{Card: card("spades", "A"), Seat: 1, IsLead: true},
		{Card: joker("small"), Seat: 2},
		{Card: joker("big"), Seat: 3},
	}
	if w := determineTrickWinner(trick, trumpSuit, trumpRank); w != 3 {
		t.Fatalf("大王应获胜，实际座位%d", w)
	}
}
