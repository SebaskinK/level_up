package models

import (
	"encoding/json"
	"fmt"
	"sort"
)

// 规则校验器：拿 game_action_logs 里记下的一整局，逐手对照 RULE.md 重放一遍，
// 把对不上的地方列出来。用来在不靠人工玩的前提下自动找 bug。

// RuleViolation 一条规则问题
type RuleViolation struct {
	Trick  int    `json:"trick"`  // 第几墩（0 表示不属于某一墩）
	Seat   int    `json:"seat"`   // 相关座位
	Kind   string `json:"kind"`   // 问题分类
	Detail string `json:"detail"` // 具体描述
}

func (v RuleViolation) String() string {
	return fmt.Sprintf("[第%d墩][座位%d][%s] %s", v.Trick, v.Seat, v.Kind, v.Detail)
}

// GameCheckResult 一整局的校验结果
type GameCheckResult struct {
	GameID     string          `json:"gameId"`
	TrumpSuit  string          `json:"trumpSuit"`
	TrumpRank  string          `json:"trumpRank"`
	DealerSeat int             `json:"dealerSeat"`
	Tricks     int             `json:"tricks"`
	Plays      int             `json:"plays"`
	TotalPoint int             `json:"totalPoints"`
	Violations []RuleViolation `json:"violations"`
}

type snapshotHand struct {
	UserID string `json:"userId"`
	IsAI   bool   `json:"isAI"`
	Cards  []Card `json:"cards"`
}

type playingStart struct {
	TrumpSuit   string                  `json:"trumpSuit"`
	TrumpRank   string                  `json:"trumpRank"`
	DealerSeat  int                     `json:"dealerSeat"`
	FriendSeat  int                     `json:"friendSeat"`
	IsSoloMode  bool                    `json:"isSoloMode"`
	FirstPlayer int                     `json:"firstPlayer"`
	BottomCards []Card                  `json:"bottomCards"`
	Hands       map[string]snapshotHand `json:"hands"`
}

type playCardsData struct {
	Cards    []Card `json:"cards"`
	IsLead   bool   `json:"is_lead"`
	PlayType string `json:"play_type"`
}

type trickCompleteResult struct {
	WinnerSeat      int `json:"winner_seat"`
	PointsCollected int `json:"points_collected"`
	NextLeader      int `json:"next_leader"`
}

// CheckGameRules 重放并校验一整局。
func CheckGameRules(gameID string) (*GameCheckResult, error) {
	logs, err := GetGameActionLogs(gameID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(logs, func(i, j int) bool { return logs[i].ID < logs[j].ID })

	var start *playingStart
	for _, l := range logs {
		if l.ActionType != "playing_start" {
			continue
		}
		var s playingStart
		if err := json.Unmarshal(l.ActionData, &s); err != nil {
			return nil, fmt.Errorf("解析 playing_start 失败: %w", err)
		}
		start = &s
		break
	}
	if start == nil {
		return nil, fmt.Errorf("这一局没有 playing_start 快照，无法重放（老对局没有这条记录）")
	}

	res := &GameCheckResult{
		GameID:     gameID,
		TrumpSuit:  start.TrumpSuit,
		TrumpRank:  start.TrumpRank,
		DealerSeat: start.DealerSeat,
	}
	add := func(trick, seat int, kind, format string, args ...interface{}) {
		res.Violations = append(res.Violations, RuleViolation{
			Trick: trick, Seat: seat, Kind: kind, Detail: fmt.Sprintf(format, args...),
		})
	}

	// 重建牌桌
	table := &GameTable{
		GameID:      gameID,
		TrumpSuit:   start.TrumpSuit,
		TrumpRank:   start.TrumpRank,
		DealerSeat:  start.DealerSeat,
		FriendSeat:  start.FriendSeat,
		IsSoloMode:  start.IsSoloMode,
		BottomCards: start.BottomCards,
		PlayerHands: map[int]*PlayerHand{},
		Status:      "playing",
	}
	for key, h := range start.Hands {
		seat := 0
		fmt.Sscanf(key, "%d", &seat)
		if seat < 1 || seat > 5 {
			add(0, 0, "快照异常", "手牌里出现非法座位号 %q", key)
			continue
		}
		table.PlayerHands[seat] = &PlayerHand{
			UserID:     h.UserID,
			SeatNumber: seat,
			Cards:      append([]Card(nil), h.Cards...),
		}
	}

	if len(table.PlayerHands) != 5 {
		add(0, 0, "快照异常", "开局手牌只有 %d 家，应为 5 家", len(table.PlayerHands))
	}
	totalCards := len(table.BottomCards)
	for seat, h := range table.PlayerHands {
		if len(h.Cards) != 31 {
			add(0, seat, "发牌张数", "开局手牌 %d 张，应为 31 张", len(h.Cards))
		}
		totalCards += len(h.Cards)
	}
	if totalCards != 162 {
		add(0, 0, "牌数守恒", "开局总牌数 %d，三副牌应为 162 张", totalCards)
	}

	// 逐手重放
	trickNo := 0
	var trickSeats []int
	leader := start.FirstPlayer
	expectedNext := leader
	totalTrickPoints := 0

	for _, l := range logs {
		switch l.ActionType {
		case "play_cards":
			var d playCardsData
			if err := json.Unmarshal(l.ActionData, &d); err != nil {
				add(trickNo+1, l.PlayerSeat, "日志损坏", "出牌记录解析失败: %v", err)
				continue
			}
			seat := l.PlayerSeat
			hand := table.PlayerHands[seat]
			if hand == nil {
				add(trickNo+1, seat, "座位异常", "日志里出现不存在的座位")
				continue
			}

			if len(table.CurrentTrick) == 0 {
				trickSeats = nil
				leader = seat
				table.TrickLeader = seat
				expectedNext = seat
			}

			// 出牌顺序：逆时针 1->5->4->3->2->1
			if seat != expectedNext {
				add(trickNo+1, seat, "出牌顺序", "应由座位 %d 出牌，实际是座位 %d", expectedNext, seat)
			}

			// 手牌守恒：出的牌必须在手上
			missing := removeCardsFromHand(hand, d.Cards)
			if len(missing) > 0 {
				add(trickNo+1, seat, "手牌守恒", "出了手上没有的牌: %s", describeCards(missing))
			}

			// 合法性：交给跟人类出牌同一套校验
			handBefore := &PlayerHand{Cards: append(append([]Card(nil), hand.Cards...), d.Cards...)}
			if err := validateCardPlay(d.Cards, table, handBefore); err != nil {
				add(trickNo+1, seat, "出牌违规", "%s（出的是 %s）", err.Error(), describeCards(d.Cards))
			}

			for _, c := range d.Cards {
				table.CurrentTrick = append(table.CurrentTrick, PlayedCard{Card: c, Seat: seat, IsLead: seat == leader})
			}
			trickSeats = append(trickSeats, seat)
			res.Plays++
			expectedNext = ((seat - 1 - 1 + 5) % 5) + 1

		case "trick_complete":
			trickNo++
			var tr trickCompleteResult
			if len(l.ResultData) > 0 {
				_ = json.Unmarshal(l.ResultData, &tr)
			}

			if len(trickSeats) != 5 {
				add(trickNo, 0, "一墩人数", "这一墩只有 %d 家出牌，应为 5 家", len(trickSeats))
			}
			if n := countPerSeat(table.CurrentTrick); len(n) > 0 {
				first := n[trickSeats[0]]
				for _, s := range trickSeats {
					if n[s] != first {
						add(trickNo, s, "张数不一致", "出了 %d 张，领出方出了 %d 张", n[s], first)
					}
				}
			}

			wantWinner := determineTrickWinner(table.CurrentTrick, table.TrumpSuit, table.TrumpRank)
			if tr.WinnerSeat != 0 && wantWinner != tr.WinnerSeat {
				add(trickNo, tr.WinnerSeat, "赢家判定",
					"日志记为座位 %d 赢，按规则应是座位 %d（牌面：%s）",
					tr.WinnerSeat, wantWinner, describeTrick(table.CurrentTrick))
			}

			wantPoints := 0
			for _, pc := range table.CurrentTrick {
				wantPoints += getCardPoints(pc.Card)
			}
			if tr.PointsCollected != wantPoints {
				add(trickNo, tr.WinnerSeat, "本墩分数",
					"日志记 %d 分，按 5=5分/10=10分/K=10分 应为 %d 分", tr.PointsCollected, wantPoints)
			}
			totalTrickPoints += wantPoints

			table.CurrentTrick = nil
			trickSeats = nil
			if tr.WinnerSeat != 0 {
				expectedNext = tr.WinnerSeat
				leader = tr.WinnerSeat
			}
		}
	}

	res.Tricks = trickNo
	res.TotalPoint = totalTrickPoints

	// 终局检查
	for seat, h := range table.PlayerHands {
		if len(h.Cards) != 0 {
			add(0, seat, "终局手牌", "打完还剩 %d 张牌: %s", len(h.Cards), describeCards(h.Cards))
		}
	}
	bottomPoints := 0
	for _, c := range table.BottomCards {
		bottomPoints += getCardPoints(c)
	}
	if totalTrickPoints+bottomPoints != 300 {
		add(0, 0, "总分守恒",
			"场上 %d 分 + 底牌 %d 分 = %d 分，三副牌应为 300 分",
			totalTrickPoints, bottomPoints, totalTrickPoints+bottomPoints)
	}

	return res, nil
}

// removeCardsFromHand 从手牌里扣掉这一手，返回手上没有的那些牌
func removeCardsFromHand(hand *PlayerHand, cards []Card) []Card {
	var missing []Card
	for _, c := range cards {
		found := -1
		for i, hc := range hand.Cards {
			if hc.Suit == c.Suit && hc.Value == c.Value && hc.Type == c.Type {
				found = i
				break
			}
		}
		if found < 0 {
			missing = append(missing, c)
			continue
		}
		hand.Cards = append(hand.Cards[:found], hand.Cards[found+1:]...)
	}
	return missing
}

func countPerSeat(trick []PlayedCard) map[int]int {
	m := map[int]int{}
	for _, pc := range trick {
		m[pc.Seat]++
	}
	return m
}

func describeCards(cards []Card) string {
	out := ""
	for i, c := range cards {
		if i > 0 {
			out += " "
		}
		out += getSuitDisplayName(c.Suit) + c.Value
	}
	return out
}

func describeTrick(trick []PlayedCard) string {
	out := ""
	last := -1
	for _, pc := range trick {
		if pc.Seat != last {
			if last != -1 {
				out += " | "
			}
			out += fmt.Sprintf("座位%d:", pc.Seat)
			last = pc.Seat
		}
		out += " " + getSuitDisplayName(pc.Card.Suit) + pc.Card.Value
	}
	return out
}

// ── 以下为对 RULE.md 各节的额外检查 ──────────────────────────

// checkScoringCards 校验分牌定义：5=5分、10=10分、K=10分，三副共 300 分
func checkScoringCards() []RuleViolation {
	var out []RuleViolation
	want := map[string]int{"5": 5, "10": 10, "K": 10}
	total := 0
	for _, suit := range []string{"spades", "hearts", "clubs", "diamonds"} {
		for _, v := range []string{"2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K", "A"} {
			c := Card{Suit: suit, Value: v, Type: "normal"}
			got := getCardPoints(c)
			if got != want[v] {
				out = append(out, RuleViolation{
					Kind:   "分牌定义",
					Detail: fmt.Sprintf("%s%s 应为 %d 分，实际 %d 分", getSuitDisplayName(suit), v, want[v], got),
				})
			}
			total += got * 3 // 三副牌
		}
	}
	if total != 300 {
		out = append(out, RuleViolation{
			Kind:   "总分守恒",
			Detail: fmt.Sprintf("三副牌总分算出来是 %d，应为 300", total),
		})
	}
	return out
}

// checkTrumpRankOrder 校验 RULE.md 5.5 的主牌等级序列：
// 大王 > 小王 > 主级牌 > 副级牌 > 主A > 主K > ... > 主2 > 所有副牌
func checkTrumpRankOrder(trumpSuit, trumpRank string) []RuleViolation {
	var out []RuleViolation
	seq := []Card{
		{Suit: "joker", Value: "big", Type: "joker"},
		{Suit: "joker", Value: "small", Type: "joker"},
		{Suit: trumpSuit, Value: trumpRank, Type: "normal"}, // 主级牌
	}
	for _, s := range []string{"spades", "hearts", "clubs", "diamonds"} {
		if s != trumpSuit {
			seq = append(seq, Card{Suit: s, Value: trumpRank, Type: "normal"}) // 副级牌
		}
	}
	for _, v := range []string{"A", "K", "Q", "J", "10", "9", "8", "7", "6", "4", "3"} {
		if v == trumpRank {
			continue
		}
		seq = append(seq, Card{Suit: trumpSuit, Value: v, Type: "normal"}) // 主花色
	}

	// 副级牌之间 RULE.md 没规定顺序，跳过相邻比较
	isSubRank := func(c Card) bool { return c.Value == trumpRank && c.Suit != trumpSuit }

	for i := 1; i < len(seq); i++ {
		prev, cur := seq[i-1], seq[i]
		if isSubRank(prev) && isSubRank(cur) {
			continue
		}
		rp := getCardRank(prev, trumpSuit, trumpRank)
		rc := getCardRank(cur, trumpSuit, trumpRank)
		if rp <= rc {
			out = append(out, RuleViolation{
				Kind: "主牌等级序列",
				Detail: fmt.Sprintf("%s 应大于 %s，实际等级 %d vs %d",
					describeCards([]Card{prev}), describeCards([]Card{cur}), rp, rc),
			})
		}
	}

	// 最小的主牌也必须大于最大的副牌
	lowestTrump := seq[len(seq)-1]
	for _, s := range []string{"spades", "hearts", "clubs", "diamonds"} {
		if s == trumpSuit {
			continue
		}
		top := Card{Suit: s, Value: "A", Type: "normal"}
		if trumpRank == "A" {
			top = Card{Suit: s, Value: "K", Type: "normal"}
		}
		if getCardRank(lowestTrump, trumpSuit, trumpRank) <= getCardRank(top, trumpSuit, trumpRank) {
			out = append(out, RuleViolation{
				Kind: "主牌大于副牌",
				Detail: fmt.Sprintf("最小的主牌 %s 没有大于副牌 %s",
					describeCards([]Card{lowestTrump}), describeCards([]Card{top})),
			})
		}
	}
	return out
}

// CheckStaticRules 校验与具体对局无关的规则常量（分值、等级序列）
func CheckStaticRules() []RuleViolation {
	out := checkScoringCards()
	for _, trumpSuit := range []string{"spades", "hearts", "clubs", "diamonds"} {
		for _, trumpRank := range []string{"2", "5", "10", "K", "A"} {
			out = append(out, checkTrumpRankOrder(trumpSuit, trumpRank)...)
		}
	}
	return out
}
