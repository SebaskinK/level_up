package models

// AIPlayer represents an AI player in the game
type AIPlayer struct {
	UserID     string
	SeatNumber int
	Hand       []Card
	IsFriend   bool
}

// isTrumpCard 判断是否是主牌（王、级牌、主花色）
func isTrumpCard(card Card, trumpSuit string, trumpRank string) bool {
	if card.Type == "joker" {
		return true
	}
	if card.Value == trumpRank {
		return true
	}
	if card.Suit == trumpSuit {
		return true
	}
	return false
}

// isSameSuitForFollow 判断一张牌能否算作「跟上了领出的花色」。
// 领出主牌时所有主牌都算同花色；领出副牌时只有同花色的非主牌才算。
func isSameSuitForFollow(card Card, leadCard Card, trumpSuit string, trumpRank string) bool {
	leadIsTrump := isTrumpCard(leadCard, trumpSuit, trumpRank)
	cardIsTrump := isTrumpCard(card, trumpSuit, trumpRank)

	if leadIsTrump {
		return cardIsTrump
	}
	return card.Suit == leadCard.Suit && !cardIsTrump
}

// getCardBaseValue returns the base value of a card (2=2, ..., A=14)
func getCardBaseValue(card Card) int {
	if card.Type == "joker" {
		if card.Value == "big" {
			return 17
		}
		return 16
	}
	values := map[string]int{
		"2": 2, "3": 3, "4": 4, "5": 5, "6": 6, "7": 7, "8": 8, "9": 9, "10": 10,
		"J": 11, "Q": 12, "K": 13, "A": 14,
	}
	return values[card.Value]
}

// DecidePlay 决定这一手出哪些牌，返回手牌下标。
// 单人模式下机器人一律走机械式策略：合法前提下永远出最小的一手。
func (ai *AIPlayer) DecidePlay(table *GameTable) []int {
	return mechanicalPlay(ai.Hand, table)
}
