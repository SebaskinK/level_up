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
	out = append(out, checkLevelUpTable()...)
	out = append(out, checkBottomMultiplierTable()...)
	for _, trumpSuit := range []string{"spades", "hearts", "clubs", "diamonds"} {
		for _, trumpRank := range []string{"2", "5", "10", "K", "A"} {
			out = append(out, checkTrumpRankOrder(trumpSuit, trumpRank)...)
		}
	}
	return out
}

// ── 对照 RULE.md 第六、七章的静态检查 ──────────────────────

// checkLevelUpTable 对照 RULE.md 7.2 / 7.3 的升级表
func checkLevelUpTable() []RuleViolation {
	var out []RuleViolation

	// RULE.md 7.2 正常局（2打3）
	normal := []struct {
		score            int
		desc             string
		dealerUp         int // 庄家方升几级
		defenderWinsHere bool
	}{
		{0, "大光", 3, true},
		{30, "小光", 2, true},
		{59, "小光", 2, true},
		{60, "小胜", 1, true},
		{119, "小胜", 1, true},
	}
	for _, c := range normal {
		got := CalculateLevelUp(c.score, false, true)
		if got != c.dealerUp {
			out = append(out, RuleViolation{
				Kind: "升级表(7.2正常局)",
				Detail: fmt.Sprintf("抓分方得 %d 分（%s），庄家方应升 %d 级，实际 %d 级",
					c.score, c.desc, c.dealerUp, got),
			})
		}
	}
	normalDefender := []struct {
		score int
		desc  string
		up    int
	}{
		{120, "反超", 1}, {179, "反超", 1},
		{180, "大胜", 2}, {239, "大胜", 2},
		{240, "完胜", 3}, {299, "完胜", 3},
		{300, "满光", 4},
	}
	for _, c := range normalDefender {
		got := CalculateLevelUp(c.score, false, false)
		if got != c.up {
			out = append(out, RuleViolation{
				Kind: "升级表(7.2正常局)",
				Detail: fmt.Sprintf("抓分方得 %d 分（%s），抓分方应升 %d 级，实际 %d 级",
					c.score, c.desc, c.up, got),
			})
		}
	}

	// RULE.md 7.3 独打局（1打4）
	solo := []struct {
		score int
		desc  string
		up    int
	}{
		{0, "大光", 9},
		{30, "小光", 6}, {59, "小光", 6},
		{60, "小胜", 3}, {119, "小胜", 3},
	}
	for _, c := range solo {
		got := CalculateLevelUp(c.score, true, true)
		if got != c.up {
			out = append(out, RuleViolation{
				Kind: "升级表(7.3独打局)",
				Detail: fmt.Sprintf("抓分方得 %d 分（%s），庄家应升 %d 级，实际 %d 级",
					c.score, c.desc, c.up, got),
			})
		}
	}
	soloDefender := []struct {
		score int
		desc  string
		up    int
	}{
		{120, "反超", 1}, {179, "反超", 1},
		{180, "惨败", 2}, {300, "惨败", 2},
	}
	for _, c := range soloDefender {
		got := CalculateLevelUp(c.score, true, false)
		if got != c.up {
			out = append(out, RuleViolation{
				Kind: "升级表(7.3独打局)",
				Detail: fmt.Sprintf("抓分方得 %d 分（%s），抓分方应升 %d 级，实际 %d 级",
					c.score, c.desc, c.up, got),
			})
		}
	}
	return out
}

// checkBottomMultiplierTable 对照 RULE.md 6.2 抠底倍数：倍数 = 2^(n-1)
func checkBottomMultiplierTable() []RuleViolation {
	var out []RuleViolation
	table := &GameTable{TrumpSuit: "hearts", TrumpRank: "2"}

	cases := []struct {
		name  string
		cards []Card
		want  int
	}{
		{"单张抠底", []Card{card2("spades", "9")}, 1},
		{"对子抠底", []Card{card2("spades", "9"), card2("spades", "9")}, 2},
		{"三张抠底", []Card{card2("spades", "9"), card2("spades", "9"), card2("spades", "9")}, 4},
		{"连对4张", []Card{
			card2("spades", "9"), card2("spades", "9"),
			card2("spades", "10"), card2("spades", "10")}, 8},
		{"连对6张", []Card{
			card2("spades", "9"), card2("spades", "9"),
			card2("spades", "10"), card2("spades", "10"),
			card2("spades", "J"), card2("spades", "J")}, 32},
	}
	for _, c := range cases {
		got := calculateBottomCardsMultiplier(c.cards, table)
		if got != c.want {
			out = append(out, RuleViolation{
				Kind:   "抠底倍数(6.2)",
				Detail: fmt.Sprintf("%s（%d张）应为 ×%d，实际 ×%d", c.name, len(c.cards), c.want, got),
			})
		}
	}
	return out
}

func card2(suit, value string) Card {
	return Card{Suit: suit, Value: value, Type: "normal"}
}

// ── 单局结算核对（RULE.md 6.2 抠底倍数 / 7.2-7.3 升级表）────────

type gameEndResult struct {
	UserID   string `json:"user_id"`
	OldLevel string `json:"old_level"`
	NewLevel string `json:"new_level"`
	IsWinner bool   `json:"is_winner"`
	Score    int    `json:"score"`
}

type bottomKickData struct {
	Kicked        bool   `json:"kicked"`
	WinnerSeat    int    `json:"winnerSeat"`
	DealerSeat    int    `json:"dealerSeat"`
	LastTrickType string `json:"lastTrickType"`
	LastTrick     []Card `json:"lastTrickCards"`
	BottomCards   []Card `json:"bottomCards"`
}

type bottomKickResult struct {
	Multiplier   int `json:"multiplier"`
	BottomPoints int `json:"bottomPoints"`
}

var levelOrder = []string{"2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K", "A"}

func levelIndex(l string) int {
	for i, v := range levelOrder {
		if v == l {
			return i
		}
	}
	return -1
}

// CheckGameSettlement 核对一局的抠底倍数与升级结果是否符合 RULE.md
func CheckGameSettlement(gameID string) ([]RuleViolation, error) {
	logs, err := GetGameActionLogs(gameID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(logs, func(i, j int) bool { return logs[i].ID < logs[j].ID })

	var out []RuleViolation
	add := func(kind, format string, args ...interface{}) {
		out = append(out, RuleViolation{Kind: kind, Detail: fmt.Sprintf(format, args...)})
	}

	var start playingStart
	hasStart := false
	friendRevealed := false
	friendSeat := 0
	calledSolo := false
	var kickData *bottomKickData
	var kickRes *bottomKickResult
	var results []gameEndResult
	hasEnd := false

	for _, l := range logs {
		switch l.ActionType {
		case "playing_start":
			if json.Unmarshal(l.ActionData, &start) == nil {
				hasStart = true
			}
		case "friend_revealed":
			friendRevealed = true
			var fr struct {
				FriendSeat int `json:"friendSeat"`
			}
			if len(l.ResultData) > 0 && json.Unmarshal(l.ResultData, &fr) == nil {
				friendSeat = fr.FriendSeat
			}
		case "call_friend":
			var cf struct {
				IsSolo bool `json:"is_solo_mode"`
			}
			if len(l.ResultData) > 0 && json.Unmarshal(l.ResultData, &cf) == nil && cf.IsSolo {
				calledSolo = true
			}
		case "bottom_kick":
			var d bottomKickData
			var r bottomKickResult
			if json.Unmarshal(l.ActionData, &d) == nil {
				kickData = &d
			}
			if len(l.ResultData) > 0 && json.Unmarshal(l.ResultData, &r) == nil {
				kickRes = &r
			}
		case "game_end":
			var wrap struct {
				Results []gameEndResult `json:"results"`
			}
			if json.Unmarshal(l.ActionData, &wrap) == nil {
				results = wrap.Results
				hasEnd = true
			}
		}
	}

	if !hasStart || !hasEnd {
		return out, nil // 没打完的局不核对结算
	}

	// 抠底倍数：RULE.md 6.2 倍数 = 2^(n-1)，多种牌型按最大牌型算
	if kickData != nil && kickRes != nil && kickData.Kicked {
		want := 1
		switch kickData.LastTrickType {
		case "single":
			want = 1
		case "pair":
			want = 2
		case "triple":
			want = 4
		case "tractor":
			want = 1 << (len(kickData.LastTrick) - 1)
		case "throw":
			counts := suitValueCounts(kickData.LastTrick)
			want = 1
			for _, c := range counts {
				if c >= 3 {
					want = 4
					break
				}
				if c >= 2 && want < 2 {
					want = 2
				}
			}
		}
		if kickRes.Multiplier != want {
			add("抠底倍数(6.2)", "抠底牌型 %s（%d张），应为 ×%d，实际 ×%d",
				kickData.LastTrickType, len(kickData.LastTrick), want, kickRes.Multiplier)
		}

		basePoints := 0
		for _, c := range kickData.BottomCards {
			basePoints += getCardPoints(c)
		}
		if kickRes.BottomPoints != basePoints*kickRes.Multiplier {
			add("抠底得分(6.2)", "底牌 %d 分 × %d 应为 %d，实际记 %d",
				basePoints, kickRes.Multiplier, basePoints*kickRes.Multiplier, kickRes.BottomPoints)
		}
	}

	// 升级：RULE.md 7.2 / 7.3
	if len(results) > 0 {
		score := results[0].Score
		// 独打的两种情形：叫的牌全在庄家自己手上(call_friend 标了 is_solo_mode)，
		// 或者叫的牌整局没被人打出来(没有 friend_revealed)。
		isSolo := calledSolo || !friendRevealed
		dealerSide := map[string]bool{}
		for seatStr, h := range start.Hands {
			seat := 0
			fmt.Sscanf(seatStr, "%d", &seat)
			if seat == start.DealerSeat {
				dealerSide[h.UserID] = true
			}
		}
		// 朋友座位以 friend_revealed 日志为准（开局快照那会儿还没亮相）
		if friendRevealed && friendSeat > 0 && friendSeat != start.DealerSeat {
			if h, ok := start.Hands[fmt.Sprintf("%d", friendSeat)]; ok {
				dealerSide[h.UserID] = true
			}
		}

		for _, r := range results {
			isDealerSide := dealerSide[r.UserID]
			// 谁赢：抓分方 >= 120 则抓分方赢
			defenderWins := score < 120
			var wantUp int
			if isDealerSide == defenderWins {
				wantUp = CalculateLevelUpPerRule(score, isSolo, defenderWins)
			} else {
				wantUp = 0
			}
			oi, ni := levelIndex(r.OldLevel), levelIndex(r.NewLevel)
			if oi < 0 || ni < 0 {
				add("升级(7.x)", "玩家 %s 的等级 %s→%s 不在 2..A 序列里", r.UserID, r.OldLevel, r.NewLevel)
				continue
			}
			gotUp := ni - oi
			// A 是顶，升到 A 就封顶，按封顶后的目标等级比
			wantIdx := oi + wantUp
			if wantIdx > len(levelOrder)-1 {
				wantIdx = len(levelOrder) - 1
			}
			if ni != wantIdx {
				side := "抓分方"
				if isDealerSide {
					side = "庄家方"
				}
				capped := ""
				if oi+wantUp > len(levelOrder)-1 {
					capped = "（封顶到A）"
				}
				add("升级(7.x)", "%s玩家 %s：抓分方得 %d 分%s，应升 %d 级到 %s%s，实际 %s→%s 升 %d 级",
					side, r.UserID, score, soloTag(isSolo), wantUp, levelOrder[wantIdx], capped,
					r.OldLevel, r.NewLevel, gotUp)
			}
		}
	}

	return out, nil
}

func soloTag(isSolo bool) string {
	if isSolo {
		return "（独打局）"
	}
	return "（正常局）"
}

// CalculateLevelUpPerRule 严格按 RULE.md 7.2 / 7.3 的表算，用来和代码实现对照
func CalculateLevelUpPerRule(score int, isSolo bool, defenderWins bool) int {
	if isSolo {
		if defenderWins { // 庄家方赢
			switch {
			case score == 0:
				return 9
			case score < 60:
				return 6
			default:
				return 3
			}
		}
		if score >= 180 {
			return 2
		}
		return 1
	}
	if defenderWins {
		switch {
		case score == 0:
			return 3
		case score < 60:
			return 2
		default:
			return 1
		}
	}
	switch {
	case score >= 300:
		return 4
	case score >= 240:
		return 3
	case score >= 180:
		return 2
	default:
		return 1
	}
}
