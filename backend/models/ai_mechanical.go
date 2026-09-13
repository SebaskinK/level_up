package models

import "sort"

// 机械式出牌：单人模式下四个机器人的唯一策略。
//
// 约定（RULE.md + 单人模式内部约定）：
//   - 牌型从小到大：单张 < 对子 < 三张 < 拖拉机，即张数越少越小（见 RULE.md 6.2 抠底倍数表）
//   - 因此领出永远出最小的一张单牌，永远不甩牌
//   - 跟牌在 RULE.md 5.3 允许的范围内取最小
//
// 「黑红花白」只是机器人自己挑牌时的偏好，用来在 RULE.md 认为一样大的牌里挑一张出来，
// 不参与任何比大小、判谁赢的逻辑。牌力一律以 RULE.md 为准。

// suitOrderValue 机器人挑牌时的花色偏好，越大越「大」：黑桃 > 红桃 > 梅花 > 方块。
func suitOrderValue(suit string) int {
	switch suit {
	case "spades":
		return 3
	case "hearts":
		return 2
	case "clubs":
		return 1
	case "diamonds":
		return 0
	}
	return -1
}

// mechRank 机器人挑牌用的等级。
// 以 RULE.md 的等级序列为准，但把三张副级牌压成同一档——RULE.md 只说「主级牌 > 副级牌」，
// 没规定副级牌之间谁大，所以对机器人来说它们一样大，改由花色偏好决定挑哪张。
func mechRank(c Card, trumpSuit, trumpRank string) int {
	r := getCardRank(c, trumpSuit, trumpRank)
	if r >= 700 && r < 800 {
		return 700
	}
	return r
}

// mechLess 判断 a 是否比 b「小」。
// getCardRank 已把副牌排在主牌之下，所以垫牌时天然先出副牌、保住主牌。
func mechLess(a, b Card, trumpSuit, trumpRank string) bool {
	ra := mechRank(a, trumpSuit, trumpRank)
	rb := mechRank(b, trumpSuit, trumpRank)
	if ra != rb {
		return ra < rb
	}
	return suitOrderValue(a.Suit) < suitOrderValue(b.Suit)
}

// ascIndices 返回按机械式顺序升序排列的手牌下标。
func ascIndices(hand []Card, idxs []int, trumpSuit, trumpRank string) []int {
	out := append([]int(nil), idxs...)
	sort.SliceStable(out, func(i, j int) bool {
		return mechLess(hand[out[i]], hand[out[j]], trumpSuit, trumpRank)
	})
	return out
}

func allIndices(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func pickCards(hand []Card, idxs []int) []Card {
	out := make([]Card, 0, len(idxs))
	for _, i := range idxs {
		out = append(out, hand[i])
	}
	return out
}

// leadInfo 取出本轮领出的牌与张数。
func leadInfo(table *GameTable) ([]Card, int) {
	if len(table.CurrentTrick) == 0 {
		return nil, 0
	}
	leadSeat := table.CurrentTrick[0].Seat
	var leadCards []Card
	for _, pc := range table.CurrentTrick {
		if pc.Seat != leadSeat {
			break
		}
		leadCards = append(leadCards, pc.Card)
	}
	return leadCards, len(leadCards)
}

// sameValueGroups 把给定下标按「同花色同点数」分组，只保留大小 >= minSize 的组，
// 并按组内牌的等级升序排列。每组截断到 minSize 张。
func sameValueGroups(hand []Card, idxs []int, minSize int, trumpSuit, trumpRank string) [][]int {
	type key struct{ suit, value string }
	buckets := map[key][]int{}
	var order []key
	for _, i := range idxs {
		k := key{hand[i].Suit, hand[i].Value}
		if _, ok := buckets[k]; !ok {
			order = append(order, k)
		}
		buckets[k] = append(buckets[k], i)
	}

	var groups [][]int
	for _, k := range order {
		g := buckets[k]
		if len(g) < minSize {
			continue
		}
		groups = append(groups, g[:minSize])
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return mechLess(hand[groups[i][0]], hand[groups[j][0]], trumpSuit, trumpRank)
	})
	return groups
}

// followCandidates 按「从小到大」的顺序列出可能的跟牌方案。
// 每个方案都会再交给 validateCardPlay 判定合法性，非法的直接跳过。
func followCandidates(hand []Card, table *GameTable, leadCards []Card, leadCount int) [][]int {
	trumpSuit, trumpRank := table.TrumpSuit, table.TrumpRank
	leadCard := leadCards[0]

	var sameSuit, other []int
	for i, c := range hand {
		if isSameSuitForFollow(c, leadCard, trumpSuit, trumpRank) {
			sameSuit = append(sameSuit, i)
		} else {
			other = append(other, i)
		}
	}
	sameSuit = ascIndices(hand, sameSuit, trumpSuit, trumpRank)
	other = ascIndices(hand, other, trumpSuit, trumpRank)

	var cands [][]int
	add := func(idxs []int) {
		if len(idxs) == leadCount {
			cands = append(cands, idxs)
		}
	}

	// 同花色不够：必须把同花色全部跟出，再用其他牌里最小的凑够张数。
	if len(sameSuit) < leadCount {
		need := leadCount - len(sameSuit)
		if len(other) >= need {
			add(append(append([]int(nil), sameSuit...), other[:need]...))
		}
		return cands
	}

	leadType := analyzeLeadCardType(leadCards, trumpSuit, trumpRank)

	switch leadType {
	case "pair":
		for _, g := range sameValueGroups(hand, sameSuit, 2, trumpSuit, trumpRank) {
			add(g)
		}

	case "triple":
		for _, g := range sameValueGroups(hand, sameSuit, 3, trumpSuit, trumpRank) {
			add(g)
		}
		// 没有三张就用「最小对子 + 最小散牌」凑
		for _, g := range sameValueGroups(hand, sameSuit, 2, trumpSuit, trumpRank) {
			used := map[int]bool{g[0]: true, g[1]: true}
			combo := append([]int(nil), g...)
			for _, i := range sameSuit {
				if len(combo) == leadCount {
					break
				}
				if !used[i] {
					combo = append(combo, i)
				}
			}
			add(combo)
		}

	case "tractor", "throw":
		// 连续的对子（拖拉机）：按起点从小到大试每一段
		groups := sameValueGroups(hand, sameSuit, 2, trumpSuit, trumpRank)
		width := leadCount / 2
		if leadCount%2 == 0 && width >= 2 {
			for start := 0; start+width <= len(groups); start++ {
				var combo []int
				for _, g := range groups[start : start+width] {
					combo = append(combo, g...)
				}
				add(combo)
			}
		}
	}

	// 保底：同花色里最小的 leadCount 张
	add(sameSuit[:leadCount])
	return cands
}

// mechanicalPlay 返回机械式出牌的手牌下标。
func mechanicalPlay(hand []Card, table *GameTable) []int {
	if len(hand) == 0 {
		return nil
	}
	trumpSuit, trumpRank := table.TrumpSuit, table.TrumpRank
	asc := ascIndices(hand, allIndices(len(hand)), trumpSuit, trumpRank)

	// 领出：最小牌型是单张，出最小的一张
	if len(table.CurrentTrick) == 0 {
		return []int{asc[0]}
	}

	leadCards, leadCount := leadInfo(table)
	if leadCount == 0 || leadCount > len(hand) {
		return []int{asc[0]}
	}

	ph := &PlayerHand{Cards: hand}
	for _, cand := range followCandidates(hand, table, leadCards, leadCount) {
		if validateCardPlay(pickCards(hand, cand), table, ph) == nil {
			return cand
		}
	}

	// 所有候选都被判非法时，退回最小的 leadCount 张，交给上层兜底。
	return asc[:leadCount]
}
