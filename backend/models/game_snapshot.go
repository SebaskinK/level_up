package models

import "time"

// 对局快照日志：把「开打那一刻」的完整牌桌写进 game_action_logs，
// 这样事后可以脱离内存状态，单靠日志重建整局、逐手校验规则。

// logPlayingStartSnapshot 在进入出牌阶段时记录一份完整快照。
// 记录内容足以重建整局：五家手牌、底牌、主花色、级牌、庄家、朋友牌。
func logPlayingStartSnapshot(table *GameTable) {
	if table == nil {
		return
	}

	hands := make(map[string]interface{}, len(table.PlayerHands))
	for seat, hand := range table.PlayerHands {
		if hand == nil {
			continue
		}
		hands[seatKey(seat)] = map[string]interface{}{
			"userId": hand.UserID,
			"isAI":   IsAIUserID(hand.UserID),
			"cards":  hand.Cards,
		}
	}

	var calledCard interface{}
	if table.HostCalledCard != nil {
		calledCard = table.HostCalledCard
	}

	LogGameAction(GameActionLogRequest{
		GameID:     table.GameID,
		ActionType: "playing_start",
		PlayerSeat: table.CurrentPlayer,
		PlayerID:   "",
		ActionData: map[string]interface{}{
			"trumpSuit":    table.TrumpSuit,
			"trumpRank":    table.TrumpRank,
			"currentLevel": table.CurrentLevel,
			"dealerSeat":   table.DealerSeat,
			"friendSeat":   table.FriendSeat,
			"isSoloMode":   table.IsSoloMode,
			"firstPlayer":  table.CurrentPlayer,
			"calledCard":   calledCard,
			"bottomCards":  table.BottomCards,
			"hands":        hands,
		},
		ResultData: map[string]interface{}{
			"handCount":   len(table.PlayerHands),
			"bottomCount": len(table.BottomCards),
		},
	})
}

func seatKey(seat int) string {
	return string(rune('0' + seat))
}

// logDealingComplete 在最后一张牌发完时记录一份发牌结果。
// DealNextCard 有两条「发完了」的分支，两条都要记，否则会漏。
func logDealingComplete(table *GameTable, numPlayers int) {
	if table == nil {
		return
	}

	hands := make([]map[string]interface{}, 0, numPlayers)
	for seat := 1; seat <= numPlayers; seat++ {
		if h, ok := table.PlayerHands[seat]; ok {
			hands = append(hands, map[string]interface{}{
				"seat": seat, "userId": h.UserID, "cards": h.Cards,
			})
		}
	}

	LogGameAction(GameActionLogRequest{
		GameID:     table.GameID,
		ActionType: "dealing_complete",
		PlayerSeat: table.StartingDealerSeat,
		ActionData: map[string]interface{}{
			"startingDealerSeat": table.StartingDealerSeat,
			"totalDealt":         table.DealtCardCount,
			"perPlayer":          table.TotalCardsPerPlayer,
			"hands":              hands,
			"bottomCards":        table.BottomCards,
		},
		ResultData: map[string]interface{}{
			"status":        table.Status,
			"callPhase":     table.CallPhase,
			"callCountdown": table.CallCountdown,
			"callRecords":   table.CallRecords,
			"passedSeats":   table.PassedSeats,
		},
	})
}

// enterFlippingPhase 统一进入「翻底牌定庄」阶段并记日志。
// 代码里原先有 5 处各自 set CallPhase="flipping"，只有 2 处记了日志，
// 实际最常走的那条（倒计时归零、无人亮庄）恰好是没记的，导致日志缺一整个阶段。
func enterFlippingPhase(table *GameTable, reason string) {
	if table == nil {
		return
	}
	table.CallPhase = "flipping"
	table.CallCountdown = 0
	table.FlipStartedAt = time.Now()

	LogGameAction(GameActionLogRequest{
		GameID:     table.GameID,
		ActionType: "enter_flipping_phase",
		PlayerSeat: table.StartingDealerSeat,
		ActionData: map[string]interface{}{
			"reason":             reason,
			"passedSeats":        table.PassedSeats,
			"callRecords":        table.CallRecords,
			"startingDealerSeat": table.StartingDealerSeat,
			"bottomCount":        len(table.BottomCards),
		},
		ResultData: map[string]interface{}{
			"callPhase":    table.CallPhase,
			"dealingPhase": table.DealingPhase,
		},
	})
}
