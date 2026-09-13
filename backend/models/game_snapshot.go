package models

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
