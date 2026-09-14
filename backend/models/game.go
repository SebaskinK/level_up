package models

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"sync"
	"time"
)

// Global game locks to prevent concurrent modifications to the same game
var gameLocks sync.Map

func getGameLock(gameID string) *sync.Mutex {
	lock, _ := gameLocks.LoadOrStore(gameID, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// GameState represents the current state of a game
type GameState struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	HostID             string    `json:"hostId"`
	PlayerIDs          []string  `json:"playerIds"`
	MaxPlayers         int       `json:"maxPlayers"`
	Status             string    `json:"status"` // waiting, playing, finished
	CurrentLevel       string    `json:"currentLevel"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
	PreviousDealerSeat int       `json:"previousDealerSeat,omitempty"` // 上一局庄家座位号
	PreviousWinnerTeam string    `json:"previousWinnerTeam,omitempty"` // 上一局获胜队伍
	PreviousFriendSeat int       `json:"previousFriendSeat,omitempty"` // 上一局朋友座位号
	PreviousIsSolo     bool      `json:"previousIsSolo,omitempty"`     // 上一局是否是1v4
}

// Card represents a playing card
type Card struct {
	Suit  string `json:"suit"`  // hearts, diamonds, clubs, spades, joker
	Value string `json:"value"` // 2-10, J, Q, K, A
	Type  string `json:"type"`  // normal, joker
}

// PlayerHand represents a player's hand and game state
type PlayerHand struct {
	UserID     string `json:"userId"`
	Cards      []Card `json:"cards"`
	SeatNumber int    `json:"seatNumber"`
	IsFriend   bool   `json:"isFriend"`  // Whether this player is the friend
	HasCalled  bool   `json:"hasCalled"` // Whether they've called a card
	Score      int    `json:"score"`     // Current round score
	Collected  []Card `json:"collected"` // Cards collected (scoring cards)
	Level      string `json:"level"`     // Player's current level
}

// GameTable represents the active game table
type GameTable struct {
	GameID             string              `json:"gameId"`
	HostID             string              `json:"hostId"`
	Status             string              `json:"status"`                       // waiting, calling, playing, finished
	CurrentLevel       string              `json:"currentLevel"`                 // Current level being played
	TrumpSuit          string              `json:"trumpSuit"`                    // Current trump suit
	HostCalledCard     *CalledCard         `json:"hostCalledCard"`               // Card host called for friend
	FriendRevealed     bool                `json:"friendRevealed"`               // Whether friend has been revealed
	FriendSeat         int                 `json:"friendSeat"`                   // Seat number of friend (when revealed)
	IsSoloMode         bool                `json:"isSoloMode"`                   // Whether this is 1v4 mode (called card is in dealer's hand/bottom)
	BottomCards        []Card              `json:"bottomCards"`                  // 7 bottom cards
	CurrentPlayer      int                 `json:"currentPlayer"`                // Current player's seat (1-5)
	CurrentTrick       []PlayedCard        `json:"currentTrick"`                 // Cards in current trick
	LastCompletedTrick []PlayedCard        `json:"lastCompletedTrick,omitempty"` // Cards from the previous completed trick
	TrickLeader        int                 `json:"trickLeader"`                  // Who led the current trick
	TrickPassCount     int                 `json:"-"`                            // Number of passes this trick
	TricksWon          [][]Card            `json:"tricksWon"`                    // All tricks won by defender team
	PlayerHands        map[int]*PlayerHand `json:"playerHands"`                  // Seat -> PlayerHand
	LastPlay           *PlayResult         `json:"lastPlay"`                     // Last play result
	CreatedAt          time.Time           `json:"createdAt"`
	UpdatedAt          time.Time           `json:"updatedAt"`

	// 亮庄/反庄相关字段（详见 rules/03-bidding.md）
	// 历史上称为"抢庄"，已统一为"亮庄"
	DealerSeat         int          `json:"dealerSeat"`         // 庄家座位号
	StartingDealerSeat int          `json:"startingDealerSeat"` // 起始发牌人座位号
	CallPhase          string       `json:"callPhase"`          // 阶段: dealing(发牌中) / counting(倒计时) / flipping(翻底) / finished
	CallCountdown      int          `json:"callCountdown"`      // 亮庄倒计时（秒）；发牌期间为 0
	CurrentCaller      int          `json:"currentCaller"`      // 当前亮庄者座位号
	TrumpRank          string       `json:"trumpRank"`          // 级牌点数（如"2"表示打2级）
	FlippedBottomCards []Card       `json:"flippedBottomCards"` // 已翻开的底牌
	CallRecords        []CallRecord `json:"callRecords"`        // 亮庄/反庄记录
	PassedSeats        []int        `json:"passedSeats"`        // 选择"不叫庄"的玩家座位号列表
	FlipStartedAt      time.Time    `json:"flipStartedAt"`      // 翻底阶段开始的时间戳；用于按 3 秒/张推算应翻数量

	// 发牌相关字段
	DealingPhase        string `json:"dealingPhase"`        // 发牌阶段: dealing, finished
	DealtCardCount      int    `json:"dealtCardCount"`      // 已发牌总张数（0..numPlayers*31）
	TotalCardsPerPlayer int    `json:"totalCardsPerPlayer"` // 每人总牌数（31）
	DealingCards        []Card `json:"-"`                   // 待发的牌（不返回给前端）
	LastDealtSeat       int    `json:"lastDealtSeat"`       // 上一张牌发给的座位号（供前端动画显示）

	// 上一局结果（用于确定下一局起始发牌人）
	PreviousResult *PreviousGameResult `json:"previousResult,omitempty"` // 上一局结果

	// 甩牌失败高亮显示
	ThrowBlocker     int    `json:"throwBlocker,omitempty"`     // 让甩牌失败的玩家座位号（用于高亮显示）
	ThrowBlockerCard string `json:"throwBlockerCard,omitempty"` // 让甩牌失败的牌描述

	// 本局结算信息（游戏结束后用于前端显示）
	TotalPoints             int          `json:"totalPoints,omitempty"`             // 闲家（抓分方）本局总得分
	RoundResults            []GameResult `json:"roundResults,omitempty"`            // 本局每个玩家的结算结果
	NextRoundCountdownStart string       `json:"nextRoundCountdownStart,omitempty"` // 下一局倒计时开始时间（ISO 8601，服务器同步）
}

// PreviousGameResult 记录上一局的结果，用于确定下一局的起始发牌人
type PreviousGameResult struct {
	WinnerTeam string `json:"winnerTeam"` // 获胜队伍: "host" 或 "guest"
	DealerSeat int    `json:"dealerSeat"` // 上一局庄家座位号
	FriendSeat int    `json:"friendSeat"` // 上一局朋友座位号（如果是2v3）
	IsSoloMode bool   `json:"isSoloMode"` // 是否是1v4模式
}

// CallRecord represents a bid for dealer
type CallRecord struct {
	Seat      int    `json:"seat"`      // 叫庄者座位号
	Suit      string `json:"suit"`      // 叫庄花色
	Rank      string `json:"rank"`      // 叫庄级牌点数
	Count     int    `json:"count"`     // 叫庄张数（1=单张，2=对子，3=三张）
	Timestamp int64  `json:"timestamp"` // 叫庄时间戳
}

// CalledCard represents the card the host calls to find a friend
type CalledCard struct {
	Suit     string `json:"suit"`
	Value    string `json:"value"`
	Position int    `json:"position"` // 第几张被打出（1=第1张，2=第2张，3=第3张）
	Count    int    `json:"count"`    // 已打出次数计数器
}

// PlayedCard represents a card played during the game
type PlayedCard struct {
	Card      Card  `json:"card"`
	Seat      int   `json:"seat"`
	IsLead    bool  `json:"isLead"`
	Timestamp int64 `json:"timestamp"`
}

// PlayResult represents the result of a card play
type PlayResult struct {
	Success       bool         `json:"success"`
	Message       string       `json:"message"`
	NextPlayer    int          `json:"nextPlayer"`
	TrickComplete bool         `json:"trickComplete"`
	TrickWinner   int          `json:"trickWinner,omitempty"`
	GameEnded     bool         `json:"gameEnded"`
	WinnerTeam    string       `json:"winnerTeam,omitempty"`
	FinalScore    int          `json:"finalScore,omitempty"`
	GameResults   []GameResult `json:"gameResults,omitempty"`
}

// In-memory game storage (in production, use Redis or similar)
var activeGames = make(map[string]*GameTable)

// 翻底牌动画节奏：每 3 秒自动翻开一张（详见 rules/03-bidding.md §3.4）
const flipBottomCardIntervalSec = 3

// StartGame initializes and starts a game with progressive card dealing
func StartGame(gameID, hostID string) (*GameTable, error) {
	game, err := GetGame(gameID)
	if err != nil {
		return nil, err
	}

	if game.HostID != hostID {
		return nil, fmt.Errorf("only host can start the game")
	}

	if len(game.PlayerIDs) != 5 && len(game.PlayerIDs) < 3 {
		return nil, fmt.Errorf("need at least 3 players to start")
	}

	if game.Status != "waiting" {
		return nil, fmt.Errorf("game already started")
	}

	// 创建并洗好牌（3副牌，共162张）
	allCards := createShuffledDeck()

	// 根据玩家数量计算发牌数量（每人31张，7张底牌）
	numPlayers := len(game.PlayerIDs)
	if numPlayers < 3 {
		numPlayers = 3
	}
	cardsToDeal := numPlayers * 31
	if cardsToDeal > 155 {
		cardsToDeal = 155 // 最多发155张（5人）
	}
	bottomCards := allCards[cardsToDeal : cardsToDeal+7]
	dealingCards := allCards[0:cardsToDeal]

	// Determine starting dealer based on previous game result
	// 规则2.2：起始发牌者根据上一局结果确定
	startingDealer := 1 // 默认值
	maxSeats := numPlayers
	if game.PreviousDealerSeat == 0 {
		// 首局：随机确定起始发牌者（只在前numPlayers个座位中选择）
		startingDealer = rand.Intn(maxSeats) + 1
	} else {
		// 有上一局结果，根据规则确定
		prevResult, err := getPreviousGameResult(gameID)
		if err != nil {
			startingDealer = rand.Intn(maxSeats) + 1
		} else {
			startingDealer = determineStartingDealer(prevResult, game.PreviousDealerSeat)
		}
	}

	// Initialize game table - 进入发牌阶段
	table := &GameTable{
		GameID:              gameID,
		HostID:              hostID,
		Status:              "dealing", // 发牌阶段
		CurrentLevel:        game.CurrentLevel,
		TrumpSuit:           "",
		HostCalledCard:      nil,
		FriendRevealed:      false,
		BottomCards:         bottomCards,
		CurrentPlayer:       startingDealer,
		TrickLeader:         startingDealer,
		CurrentTrick:        make([]PlayedCard, 0),
		TricksWon:           make([][]Card, 0),
		PlayerHands:         make(map[int]*PlayerHand),
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
		StartingDealerSeat:  startingDealer,
		CurrentCaller:       startingDealer,
		CallPhase:           "dealing", // 发牌中
		CallCountdown:       0,         // 发牌期间不倒计时；发完最后一张才启动 10 秒
		TrumpRank:           game.CurrentLevel,
		FlippedBottomCards:  make([]Card, 0),
		CallRecords:         make([]CallRecord, 0),
		DealingPhase:        "dealing",
		DealtCardCount:      0,
		TotalCardsPerPlayer: 31,
		DealingCards:        dealingCards,
	}

	// 初始化玩家手牌为空（逐张发牌）
	for i, playerID := range game.PlayerIDs {
		seat := i + 1
		table.PlayerHands[seat] = &PlayerHand{
			UserID:     playerID,
			Cards:      make([]Card, 0), // 初始为空
			SeatNumber: seat,
			IsFriend:   false,
			Score:      0,
			Collected:  make([]Card, 0),
		}
	}

	// Store active game
	activeGames[gameID] = table

	// Update game status in database
	UpdateGameStatus(gameID, "playing")

	// Log game start action
	LogGameAction(GameActionLogRequest{
		GameID:     gameID,
		ActionType: "game_start",
		PlayerSeat: 0,
		PlayerID:   hostID,
		ActionData: map[string]interface{}{
			"starting_dealer": startingDealer,
			"current_level":   game.CurrentLevel,
			"player_count":    len(game.PlayerIDs),
			"dealing_mode":    "progressive",
		},
		ResultData: map[string]interface{}{
			"status": "success",
		},
	})

	return table, nil
}

// createShuffledDeck 创建并洗好的牌堆（3副牌，162张）
func createShuffledDeck() []Card {
	var allCards []Card
	suits := []string{"hearts", "diamonds", "clubs", "spades"}
	values := []string{"2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K", "A"}

	for deck := 0; deck < 3; deck++ {
		for _, suit := range suits {
			for _, value := range values {
				allCards = append(allCards, Card{
					Suit:  suit,
					Value: value,
					Type:  "normal",
				})
			}
		}
		// 添加王牌
		allCards = append(allCards, Card{Suit: "joker", Value: "small", Type: "joker"})
		allCards = append(allCards, Card{Suit: "joker", Value: "big", Type: "joker"})
	}

	// 洗牌
	rand.Shuffle(len(allCards), func(i, j int) {
		allCards[i], allCards[j] = allCards[j], allCards[i]
	})

	return allCards
}

// DealNextCard 按规则的逆时针顺序发出"下一张"牌
// 返回值：(更新后的table, 是否发牌完成, error)
// 顺序：从 StartingDealerSeat 开始，按逆时针 1→5→4→3→2→1... 每次发一张
// 每位玩家共 TotalCardsPerPlayer (31) 张，5 人共 155 张
func DealNextCard(gameID string) (*GameTable, bool, error) {
	table, exists := activeGames[gameID]
	if !exists {
		return nil, false, ErrGameNotFound
	}

	if table.DealingPhase != "dealing" {
		return table, true, nil // 发牌已完成
	}

	numPlayers := len(table.PlayerHands)
	if numPlayers <= 0 {
		numPlayers = 5
	}
	totalCards := numPlayers * table.TotalCardsPerPlayer

	// 检查是否发完
	if table.DealtCardCount >= totalCards {
		table.DealingPhase = "finished"
		table.Status = "calling"
		// 发完最后一张牌的瞬间处理倒计时/翻底（详见 rules/03-bidding.md §3.0 §3.1 §3.4）：
		// - 发牌期间已有人亮庄并封顶（Count==3）：跳过倒计时，直接 finalize 切到扣底牌
		// - 发牌期间所有人已按"不叫庄"(PassedSeats==totalPlayers 且无 CallRecords)：直接进入翻底阶段
		// - 其它情况（包括"发牌期间已有人亮庄但未封顶"）：统一启动 10 秒初始倒计时
		//   依据规则文档 §3.1：初始倒计时永远是 10 秒，发牌期间的亮庄不视作"倒计时内的亮庄"，
		//   故不应覆盖为 5 秒；后续若有人在这 10 秒内再亮/反庄，才会刷新为 5 秒。
		hasCall := len(table.CallRecords) > 0
		allPassed := len(table.PassedSeats) == numPlayers
		isMaxCalled := hasCall && table.CallRecords[len(table.CallRecords)-1].Count == 3
		switch {
		case isMaxCalled:
			table.UpdatedAt = time.Now()
			activeGames[gameID] = table
			finalized, ferr := finalizeDealerAndStartPlaying(table)
			if ferr != nil {
				return finalized, true, ferr
			}
			return finalized, true, nil
		case !hasCall && allPassed:
			table.CallPhase = "flipping"
			table.CallCountdown = 0
			table.FlipStartedAt = time.Now()
		default:
			table.CallPhase = "counting"
			table.CallCountdown = 10
		}
		table.UpdatedAt = time.Now()
		activeGames[gameID] = table

		// 单人模式：发牌完成后自动为人类玩家亮级牌（如果有的话）
		// TODO: 临时禁用，用于测试倒计时→翻底牌流程
		// if isSinglePlayerGame(table) {
		// 	autoCallForHumanIfPossible(table, gameID)
		// }

		log.Printf("[DealNextCard] Dealing complete for game %s, entering calling phase (countdown=%d, phase=%s)",
			gameID, table.CallCountdown, table.CallPhase)

		logDealingComplete(table, numPlayers)

		return table, true, nil
	}

	// 计算这一张应该发给哪个座位（逆时针）
	// 逆时针在座位 1..numPlayers 中：next = ((seat - 2 + numPlayers) % numPlayers) + 1
	// 即 1→5→4→3→2→1...（5人桌）
	seat := table.StartingDealerSeat
	if seat <= 0 {
		seat = 1
	}
	step := table.DealtCardCount % numPlayers
	for k := 0; k < step; k++ {
		seat = ((seat - 2 + numPlayers) % numPlayers) + 1
	}

	// 取下一张待发的牌
	cardIndex := table.DealtCardCount
	if cardIndex < len(table.DealingCards) {
		card := table.DealingCards[cardIndex]
		if hand, ok := table.PlayerHands[seat]; ok {
			hand.Cards = append(hand.Cards, card)
		}
	}

	table.DealtCardCount++
	table.LastDealtSeat = seat
	table.UpdatedAt = time.Now()
	activeGames[gameID] = table

	log.Printf("[DealNextCard] game=%s dealt card #%d to seat %d", gameID, table.DealtCardCount, seat)

	// 发完最后一张牌后立即触发完成逻辑
	if table.DealtCardCount >= totalCards {
		table.DealingPhase = "finished"
		// 若已经被 finalize 推进到 discarding 等阶段（例如发牌中三张封顶定庄），
		// 不要把 Status / CallPhase / CallCountdown 重置回 calling 流程。
		if table.Status == "dealing" || table.Status == "calling" {
			table.Status = "calling"
			hasCall := len(table.CallRecords) > 0
			allPassed := len(table.PassedSeats) == numPlayers
			isMaxCalled := hasCall && table.CallRecords[len(table.CallRecords)-1].Count == 3
			switch {
			case isMaxCalled:
				table.UpdatedAt = time.Now()
				activeGames[gameID] = table
				logDealingComplete(table, numPlayers)
				finalized, ferr := finalizeDealerAndStartPlaying(table)
				if ferr != nil {
					return finalized, true, ferr
				}
				return finalized, true, nil
			case !hasCall && allPassed:
				table.CallPhase = "flipping"
				table.CallCountdown = 0
				table.FlipStartedAt = time.Now()
			default:
				table.CallPhase = "counting"
				table.CallCountdown = 10
			}
		}
		table.UpdatedAt = time.Now()
		activeGames[gameID] = table

		// 单人模式：发牌完成后自动为人类玩家亮级牌（如果有的话）
		// TODO: 临时禁用，用于测试倒计时→翻底牌流程
		// if isSinglePlayerGame(table) {
		// 	autoCallForHumanIfPossible(table, gameID)
		// }

		log.Printf("[DealNextCard] Dealing complete for game %s, entering calling phase (countdown=%d, phase=%s)",
			gameID, table.CallCountdown, table.CallPhase)
		logDealingComplete(table, numPlayers)
		return table, true, nil
	}

	return table, false, nil
}

// autoCallForHumanIfPossible 单人模式发牌完成后，自动为人类玩家亮级牌
func autoCallForHumanIfPossible(table *GameTable, gameID string) {
	playerHand := table.PlayerHands[1]
	if playerHand == nil {
		return
	}
	rankCards := findRankCards(playerHand.Cards, table.CurrentLevel)
	if len(rankCards) == 0 {
		return
	}
	suitCounts := make(map[string][]int)
	for idx, card := range playerHand.Cards {
		if card.Value == table.CurrentLevel {
			suitCounts[card.Suit] = append(suitCounts[card.Suit], idx)
		}
	}
	var bestSuit string
	var bestIndices []int
	for suit, indices := range suitCounts {
		if len(indices) > len(bestIndices) {
			bestSuit = suit
			bestIndices = indices
		}
	}
	if len(bestIndices) > 0 {
		CallDealer(gameID, playerHand.UserID, bestSuit, bestIndices[:1])
	}
}

// GetTableGame retrieves the active game table
func GetTableGame(gameID string) (*GameTable, error) {
	table, exists := activeGames[gameID]
	if !exists {
		// Try to load from database
		game, err := GetGame(gameID)
		if err != nil {
			return nil, ErrGameNotFound
		}
		if game.Status == "waiting" {
			return &GameTable{
				GameID:       gameID,
				HostID:       game.HostID,
				Status:       "waiting",
				PlayerHands:  make(map[int]*PlayerHand),
				CurrentTrick: make([]PlayedCard, 0),
			}, nil
		}
		return nil, fmt.Errorf("game not active in memory")
	}

	// 自动递减倒计时（基于UpdatedAt时间戳）
	// 规则（rules/03-bidding.md §3.0）：发牌期间 (DealingPhase=="dealing") 不倒计时，
	// 只有当 Status=="calling" 且 CallPhase=="counting" 时才走倒计时。
	inBiddingWindow := table.Status == "calling" && table.CallPhase == "counting"
	if inBiddingWindow && table.CallCountdown > 0 {
		elapsed := int(time.Since(table.UpdatedAt).Seconds())
		if elapsed > 0 {
			table.CallCountdown -= elapsed
			if table.CallCountdown < 0 {
				table.CallCountdown = 0
			}
			table.UpdatedAt = time.Now()
			// 更新内存中的倒计时
			activeGames[gameID] = table
			log.Printf("[GetTableGame] Countdown updated: %d seconds remaining (status=%s, phase=%s)",
				table.CallCountdown, table.Status, table.CallPhase)
		}
	}

	// 自动翻底牌（按每 3 秒一张推算，详见 rules/03-bidding.md §3.4）
	// 当 CallPhase=="flipping" 时，根据 FlipStartedAt 推算应当翻开多少张，
	// 缺多少张就在这里补翻几次（中途翻到级牌即停止）。
	if table.Status == "calling" && table.CallPhase == "flipping" {
		if table.FlipStartedAt.IsZero() {
			table.FlipStartedAt = time.Now()
		}
		elapsedSec := int(time.Since(table.FlipStartedAt).Seconds())
		expectedFlipped := elapsedSec/flipBottomCardIntervalSec + 1 // 进入 flipping 立刻翻第一张
		if expectedFlipped > len(table.BottomCards) {
			expectedFlipped = len(table.BottomCards)
		}
		for len(table.FlippedBottomCards) < expectedFlipped && table.CallPhase == "flipping" {
			updated, err := flipNextBottomCardCore(table, gameID)
			if err != nil {
				break
			}
			table = updated
		}
		activeGames[gameID] = table
	}

	// finished 阶段保留 3 秒：留给前端最后一张翻牌动画 + 定庄结果短暂展示，
	// 然后 tick 推进到 discarding（call_records 路径下 finalize 已立即执行，此处只处理 flipping → finished 的等待）。
	if table.Status == "calling" && table.CallPhase == "finished" {
		if time.Since(table.UpdatedAt) >= 3*time.Second {
			log.Printf("[GetTableGame] finished phase elapsed >= 3s, finalize and start playing")
			return finalizeDealerAndStartPlaying(table)
		}
	}

	// 扣底牌阶段：懒加载地把底牌并入庄家手牌。
	// 此处而非 finalizeDealerAndStartPlaying 中合入，是为了让前端在
	// "定庄成功"展示窗口（仍处于 calling/finished）期间看到的庄家手牌保持 31 张，
	// 只有真正进入 discarding 阶段后下一次 GetTableGame 才返回 38 张，
	// 即手牌区只有在扣底牌阶段才会出现底牌。
	if table.Status == "discarding" {
		ensureDealerHasBottomCards(table)
		activeGames[gameID] = table
	}

	// 当倒计时为0且有人亮庄时，自动确定庄家
	// 注意：只有在发牌已完成（Status=="calling"）时才确定庄家；
	// 发牌期间倒计时归零仍要等牌发完再处理，避免提前结束流程
	if table.Status == "calling" && table.CallPhase == "counting" && table.CallCountdown <= 0 {
		if len(table.CallRecords) > 0 {
			// 有人亮庄，倒计时结束，确定庄家
			lastCall := table.CallRecords[len(table.CallRecords)-1]
			table.DealerSeat = lastCall.Seat
			table.HostID = table.PlayerHands[lastCall.Seat].UserID
			table.TrumpSuit = lastCall.Suit
			table.TrumpRank = lastCall.Rank
			log.Printf("[GetTableGame] Countdown ended, dealer confirmed: seat=%d, trumpSuit=%s, trumpRank=%s",
				table.DealerSeat, table.TrumpSuit, table.TrumpRank)

			// 记录确定庄家的日志
			LogGameAction(GameActionLogRequest{
				GameID:     gameID,
				ActionType: "dealer_confirmed",
				PlayerSeat: 0,
				PlayerID:   "",
				ActionData: map[string]interface{}{
					"reason": "countdown_ended",
				},
				ResultData: map[string]interface{}{
					"dealer_seat": table.DealerSeat,
					"trump_suit":  table.TrumpSuit,
					"trump_rank":  table.TrumpRank,
				},
			})

			// 亮庄定庄不展示底牌，与翻底定庄互斥：倒计时归零后立即进入扣底牌阶段，中间不停顿。
			table.UpdatedAt = time.Now()
			activeGames[gameID] = table
			return finalizeDealerAndStartPlaying(table)
		}

		// 无人亮庄，倒计时结束，进入翻底牌阶段
		table.CallPhase = "flipping"
		table.FlipStartedAt = time.Now()
		log.Printf("[GetTableGame] Countdown ended with no caller, entering flipping phase")
		activeGames[gameID] = table
	}

	return table, nil
}

// CallFriendCard sets the card the host calls to find their friend
// position: 第几张被打出的牌成为盟友（1=第1张，2=第2张，3=第3张）
// 如果叫的牌在庄家手中或底牌中达不到position张数，则触发1打4独打模式
func CallFriendCard(gameID, userID, suit, value string, position int) (*GameTable, error) {
	table, err := GetTableGame(gameID)
	if err != nil {
		return nil, err
	}

	if table.HostID != userID {
		return nil, fmt.Errorf("only host can call friend")
	}

	if table.Status != "playing" && table.Status != "calling_friend" {
		return nil, fmt.Errorf("游戏不在进行中或叫朋友阶段")
	}

	if position < 1 || position > 3 {
		return nil, fmt.Errorf("position must be between 1 and 3")
	}

	// 叫牌限制：不可以叫本轮场上已经亮过的牌（叫庄、反庄时亮出的牌）
	// 检查CallRecords中记录的所有叫庄、反庄时亮出的牌
	// 规则：如果是级牌，且与之前叫庄/反庄的花色相同，则不能叫
	for _, record := range table.CallRecords {
		// record.Rank 是级牌点数，record.Suit 是叫庄时用的花色
		// 例如：用红桃2叫庄，则记录为 {Rank: "2", Suit: "hearts"}
		// 表示亮出了红桃2，不能再叫红桃2，但可以叫方片2
		if record.Rank == value && record.Suit == suit {
			return nil, fmt.Errorf("不可以叫本轮场上已经亮过的牌: %s%s", suit, value)
		}
	}

	table.HostCalledCard = &CalledCard{
		Suit:     suit,
		Value:    value,
		Position: position,
		Count:    0, // 初始化计数器
	}

	// 统计该牌在庄家手中和底牌中的总数
	totalCount := 0
	dealerHand, ok := table.PlayerHands[table.DealerSeat]
	if !ok {
		return nil, fmt.Errorf("dealer hand not found")
	}

	// 统计庄家手牌中该牌的数量
	for _, card := range dealerHand.Cards {
		if card.Suit == suit && card.Value == value {
			totalCount++
		}
	}

	// 统计底牌中该牌的数量
	for _, card := range table.BottomCards {
		if card.Suit == suit && card.Value == value {
			totalCount++
		}
	}

	// 如果庄家手中+底牌中的该牌数量 >= position，说明无法打出第position张，触发1打4
	// 例如：叫第3张红桃A，庄家手中有2张，底牌有1张，总共3张，无法让其他玩家打出第3张
	if totalCount >= position {
		table.IsSoloMode = true
		table.FriendRevealed = true
		table.FriendSeat = table.DealerSeat // 庄家自己就是"朋友"
		table.UpdatedAt = time.Now()

		// 记录叫朋友日志（1v4模式）
		LogGameAction(GameActionLogRequest{
			GameID:     gameID,
			ActionType: "call_friend",
			PlayerSeat: table.DealerSeat,
			PlayerID:   userID,
			ActionData: map[string]interface{}{
				"suit":                suit,
				"value":               value,
				"position":            position,
				"called_card":         fmt.Sprintf("%s%s", suit, value),
				"card_in_dealer_hand": totalCount,
			},
			ResultData: map[string]interface{}{
				"is_solo_mode":    true,
				"friend_revealed": true,
				"friend_seat":     table.DealerSeat,
				"game_mode":       "1v4",
				"reason":          "called_card_not_reachable",
			},
		})

		// 独打模式下叫朋友完成后直接进入出牌阶段
		// （因为已经先扣底再叫朋友，所以这里直接进入playing）
		if table.Status == "calling_friend" {
			table.Status = "playing"
			table.CurrentPlayer = table.DealerSeat
			table.CallPhase = "finished"
			table.UpdatedAt = time.Now()
			logPlayingStartSnapshot(table)
		}

		// Save the updated table state
		activeGames[gameID] = table

		return table, nil
	}

	// 正常2打3模式
	table.IsSoloMode = false
	table.UpdatedAt = time.Now()

	// 记录叫朋友日志
	LogGameAction(GameActionLogRequest{
		GameID:     gameID,
		ActionType: "call_friend",
		PlayerSeat: table.DealerSeat,
		PlayerID:   userID,
		ActionData: map[string]interface{}{
			"suit":        suit,
			"value":       value,
			"position":    position,
			"called_card": fmt.Sprintf("%s%s", suit, value),
		},
		ResultData: map[string]interface{}{
			"is_solo_mode":    table.IsSoloMode,
			"friend_revealed": table.FriendRevealed,
			"game_mode":       "2v3",
		},
	})

	// 如果之前是calling_friend状态，叫朋友完成后直接进入出牌阶段
	// （因为已经先扣底再叫朋友，所以这里直接进入playing）
	if table.Status == "calling_friend" {
		table.Status = "playing"
		table.CurrentPlayer = table.DealerSeat
		table.CallPhase = "finished"
		table.UpdatedAt = time.Now()
		logPlayingStartSnapshot(table)
	}

	// Save the updated table state
	activeGames[gameID] = table

	return table, nil
}

// PlayCardGame plays a card from a player's hand
func PlayCardGame(gameID, userID string, cardIndex int) (*PlayResult, error) {
	table, err := GetTableGame(gameID)
	if err != nil {
		return nil, err
	}

	if table.Status != "playing" {
		return nil, fmt.Errorf("游戏不在进行中")
	}

	// Find player's seat
	var playerSeat int
	var hand *PlayerHand
	for seat, h := range table.PlayerHands {
		if h.UserID == userID {
			playerSeat = seat
			hand = h
			break
		}
	}

	if hand == nil {
		return nil, fmt.Errorf("player not in game")
	}

	if playerSeat != table.CurrentPlayer {
		return nil, fmt.Errorf("not your turn")
	}

	// Validate card index
	if cardIndex < 0 || cardIndex >= len(hand.Cards) {
		return nil, fmt.Errorf("invalid card index")
	}

	card := hand.Cards[cardIndex]

	// Check for friend reveal (when called card is played)
	// 追踪打出次数，当打出第N张时识别盟友
	if !table.FriendRevealed && table.HostCalledCard != nil {
		if card.Suit == table.HostCalledCard.Suit && card.Value == table.HostCalledCard.Value {
			// 打出了叫的牌，计数器+1
			table.HostCalledCard.Count++

			// 检查是否达到或超过指定的position
			if table.HostCalledCard.Count >= table.HostCalledCard.Position {
				// 第N张被打出，识别盟友
				table.FriendRevealed = true
				table.FriendSeat = playerSeat
				hand.IsFriend = true
			}
		}
	}

	// Remove card from hand and add to current trick
	hand.Cards = append(hand.Cards[:cardIndex], hand.Cards[cardIndex+1:]...)
	isLead := len(table.CurrentTrick) == 0
	if isLead {
		table.TrickLeader = playerSeat
	}

	table.CurrentTrick = append(table.CurrentTrick, PlayedCard{
		Card:      card,
		Seat:      playerSeat,
		IsLead:    isLead,
		Timestamp: time.Now().UnixNano(),
	})

	table.UpdatedAt = time.Now()

	result := &PlayResult{
		Success: true,
		Message: fmt.Sprintf("Played %s of %s", card.Value, card.Suit),
	}

	// Check if trick is complete (5 cards played)
	if len(table.CurrentTrick) == 5 {
		winner := determineTrickWinner(table.CurrentTrick, table.TrumpSuit, table.TrumpRank)
		result.TrickComplete = true
		result.TrickWinner = winner

		// 当轮结束，清除甩牌失败高亮
		table.ThrowBlocker = 0
		table.ThrowBlockerCard = ""

		// Collect scoring cards
		var collectedCards []Card
		for _, pc := range table.CurrentTrick {
			if isScoringCard(pc.Card) {
				collectedCards = append(collectedCards, pc.Card)
			}
		}

		// Winner gets the cards
		if winnerHand, ok := table.PlayerHands[winner]; ok {
			winnerHand.Collected = append(winnerHand.Collected, collectedCards...)
		}

		table.TricksWon = append(table.TricksWon, []Card{card})

		// Clear trick and set winner as next leader
		// Save current trick to last completed trick before clearing
		table.LastCompletedTrick = make([]PlayedCard, len(table.CurrentTrick))
		copy(table.LastCompletedTrick, table.CurrentTrick)
		table.CurrentTrick = make([]PlayedCard, 0)
		table.CurrentPlayer = winner
		table.TrickLeader = winner
	} else {
		// Next player (counter-clockwise: 1→5→4→3→2→1)
		result.NextPlayer = ((playerSeat - 2 + 5) % 5) + 1
		table.CurrentPlayer = result.NextPlayer
	}

	table.LastPlay = result
	return result, nil
}

// determineTrickWinner determines who wins the current trick
// 新规则：必须同牌型才能毙牌（对子毙对子，三张毙三张等）
// 王作为主牌的一部分，不存在"王毙主牌"
func determineTrickWinner(trick []PlayedCard, trumpSuit, trumpRank string) int {
	if len(trick) == 0 {
		return 0
	}

	// 按座位分组牌
	playsByPlayer := make(map[int][]Card)
	playerOrder := []int{} // 记录出牌顺序
	for _, pc := range trick {
		if _, exists := playsByPlayer[pc.Seat]; !exists {
			playerOrder = append(playerOrder, pc.Seat)
		}
		playsByPlayer[pc.Seat] = append(playsByPlayer[pc.Seat], pc.Card)
	}

	// 领出玩家和领出牌
	leadPlayer := playerOrder[0]
	leadCards := playsByPlayer[leadPlayer]
	leadSuit := leadCards[0].Suit

	// 判断领出牌型
	leadCardType := determineCardType(leadCards, trumpSuit, trumpRank)

	// 初始化赢家为领出玩家
	winner := leadPlayer
	winnerCards := leadCards

	// 遍历其他玩家的出牌
	for i := 1; i < len(playerOrder); i++ {
		player := playerOrder[i]
		cards := playsByPlayer[player]

		// 判断跟牌的牌型
		cardType := determineCardType(cards, trumpSuit, trumpRank)

		// 只有牌型匹配才能参与比较
		if cardType != leadCardType {
			continue // 牌型不匹配，无法毙牌
		}

		// 牌型匹配，比较大小
		// 规则：主牌（含王）> 副牌
		if canBeatCards(cards, winnerCards, leadSuit, trumpSuit, trumpRank) {
			winner = player
			winnerCards = cards
		}
	}

	return winner
}

// determineCardType 判断牌型
// 考虑级牌跳过的情况
func determineCardType(cards []Card, trumpSuit, trumpRank string) string {
	if len(cards) == 1 {
		return "single"
	}
	if len(cards) == 2 && cards[0].Value == cards[1].Value && cards[0].Suit == cards[1].Suit {
		return "pair"
	}
	if len(cards) == 3 && cards[0].Value == cards[1].Value && cards[1].Value == cards[2].Value &&
		cards[0].Suit == cards[1].Suit && cards[1].Suit == cards[2].Suit {
		return "triple"
	}
	if len(cards) >= 4 && isTractorWithContext(cards, trumpSuit, trumpRank) {
		return "tractor"
	}
	// 甩牌或其他组合
	return "throw"
}

// canBeatCards 判断 cards 是否能击败 currentWinnerCards
// 规则：主牌（含王）> 副牌；副牌之间只有跟上领出花色的才算数（RULE.md 5.3 规则4 / 5.4）
func canBeatCards(cards []Card, currentWinnerCards []Card, leadSuit, trumpSuit, trumpRank string) bool {
	// 垫出去的其他副牌无法参与比较，领出方获胜
	if !playCanCompete(cards, leadSuit, trumpSuit, trumpRank) {
		return false
	}

	// 判断两组牌的花色类型
	cardsType := getPlayType(cards, trumpSuit, trumpRank)
	winnerType := getPlayType(currentWinnerCards, trumpSuit, trumpRank)

	// 主牌 > 副牌
	if cardsType == "trump" && winnerType != "trump" {
		return true // 主牌毙副牌
	}
	if cardsType != "trump" && winnerType == "trump" {
		return false // 副牌无法毙主牌
	}

	// 同为主牌或同为副牌，比较最大牌的等级
	maxCard1 := getMaxCardRank(cards, trumpSuit, trumpRank)
	maxCard2 := getMaxCardRank(currentWinnerCards, trumpSuit, trumpRank)

	return maxCard1 > maxCard2
}

// playCanCompete 判断一手牌有没有资格参与比大小。
// 只有「整手都是主牌」或「整手都是领出花色」才算数；
// 花色不齐的那一手是垫牌，按 RULE.md 5.4 无法毙牌。
func playCanCompete(cards []Card, leadSuit, trumpSuit, trumpRank string) bool {
	allTrump := true
	allLeadSuit := true
	for _, card := range cards {
		if isTrumpCard(card, trumpSuit, trumpRank) {
			allLeadSuit = false
		} else {
			allTrump = false
			if card.Suit != leadSuit {
				allLeadSuit = false
			}
		}
	}
	return allTrump || allLeadSuit
}

// getPlayType 判断出牌类型：trump（主牌）或 suit（副牌）
// 王、级牌、主花色牌都算主牌
func getPlayType(cards []Card, trumpSuit, trumpRank string) string {
	for _, card := range cards {
		if isTrumpCard(card, trumpSuit, trumpRank) {
			return "trump"
		}
	}
	return "suit" // 副牌
}

// getMaxCardRank 获取一组牌中最大牌的等级
func getMaxCardRank(cards []Card, trumpSuit, trumpRank string) int {
	maxRank := 0
	for _, card := range cards {
		rank := getCardRank(card, trumpSuit, trumpRank)
		if rank > maxRank {
			maxRank = rank
		}
	}
	return maxRank
}

// getCardValue returns the numeric value of a card for comparison
// getCardRank 计算牌在游戏中的等级，考虑主牌、级牌等特殊规则
// 返回值越大，牌的等级越高
// 主牌等级: 大王(1000) > 小王(900) > 主级牌(800) > 副级牌(700-703) > 主A(614) > 主K(613) > ... > 主3(603)
// 副牌等级: A(14) > K(13) > ... > 3(3) > 2(2) (跳过级牌)
func getCardRank(card Card, trumpSuit, trumpRank string) int {
	// 1. 大王
	if card.Type == "joker" && card.Value == "big" {
		return 1000
	}

	// 2. 小王
	if card.Type == "joker" && card.Value == "small" {
		return 900
	}

	// 3. 主级牌（主花色的级牌）
	if card.Value == trumpRank && card.Suit == trumpSuit {
		return 800
	}

	// 4. 副级牌（其他花色的级牌）
	// RULE.md 只规定「主级牌 > 副级牌」，没有规定三张副级牌之间谁大，
	// 这里沿用历史顺序，不要改成机器人选牌用的「黑红花白」——那只是选牌偏好，不是牌力规则。
	if card.Value == trumpRank {
		suitOrder := map[string]int{
			"spades":   3,
			"hearts":   2,
			"diamonds": 1,
			"clubs":    0,
		}
		return 700 + suitOrder[card.Suit]
	}

	// 5. 主花色牌（非级牌）
	if card.Suit == trumpSuit {
		baseValues := map[string]int{
			"2": 2, "3": 3, "4": 4, "5": 5, "6": 6, "7": 7, "8": 8, "9": 9, "10": 10,
			"J": 11, "Q": 12, "K": 13, "A": 14,
		}
		return 600 + baseValues[card.Value]
	}

	// 6. 副牌（非级牌）
	baseValues := map[string]int{
		"2": 2, "3": 3, "4": 4, "5": 5, "6": 6, "7": 7, "8": 8, "9": 9, "10": 10,
		"J": 11, "Q": 12, "K": 13, "A": 14,
	}
	return baseValues[card.Value]
}

func getCardValue(card Card, leadSuit, trumpSuit string) int {
	values := map[string]int{
		"2": 2, "3": 3, "4": 4, "5": 5, "6": 6, "7": 7, "8": 8, "9": 9, "10": 10,
		"J": 11, "Q": 12, "K": 13, "A": 14,
	}

	baseValue := values[card.Value]

	// Trump cards are highest
	if trumpSuit != "" && card.Suit == trumpSuit {
		return baseValue + 100
	}

	// Big joker is highest of all
	if card.Type == "joker" && card.Value == "big" {
		return 200
	}

	// Small joker
	if card.Type == "joker" && card.Value == "small" {
		return 150
	}

	// Lead suit cards beat non-lead, non-trump
	if card.Suit == leadSuit {
		return baseValue
	}

	// Non-lead, non-trump cards are lowest
	return baseValue - 100
}

// isScoringCard checks if a card is worth points
// 规则：所有花色的5、10、K都是分值牌（总分300分）
func isScoringCard(card Card) bool {
	// 所有花色的5、10、K都是分值牌
	return card.Value == "5" || card.Value == "10" || card.Value == "K"
}

// getCardPoints returns the point value of a scoring card
// 规则：所有花色的5=5分，10=10分，K=10分（3副牌共300分）
func getCardPoints(card Card) int {
	// 所有花色的5、10、K都是分值牌
	if card.Value == "5" {
		return 5 // 5 = 5分，三副牌共60分 (4花色 × 3副 × 5分)
	}
	if card.Value == "10" || card.Value == "K" {
		return 10 // 10和K = 10分，三副牌各120分 (4花色 × 3副 × 10分)
	}
	return 0
}

// calculateBottomCardsMultiplier 计算抠底倍数
// 规则6.2：倍数 = 2^(n-1)，其中 n 为牌型的张数
// - 单张抠底：2^(1-1) = 1
// - 对子抠底：2^(2-1) = 2
// - 三张抠底：2^(3-1) = 4
// - 拖拉机抠底（4张）：2^(4-1) = 8
// - 拖拉机抠底（6张）：2^(6-1) = 32
//
// 计算规则：
// 1. 先分析牌型（三张、对子、单张）
// 2. 优先选择最大的牌型（三张 > 对子 > 单张）
// 3. 根据最大牌型的张数计算倍数
func calculateBottomCardsMultiplier(cards []Card, table *GameTable) int {
	if len(cards) == 0 {
		return 1
	}

	// 判断牌型
	cardType := determineCardType(cards, table.TrumpSuit, table.TrumpRank)

	switch cardType {
	case "single":
		// 单张：2^(1-1) = 1
		return 1
	case "pair":
		// 对子：2^(2-1) = 2
		return 2
	case "triple":
		// 三张：2^(3-1) = 4
		return 4
	case "tractor":
		// 拖拉机：2^(n-1)，n 为总牌数
		// 例如：4张（2对）= 2^3 = 8，6张（3对）= 2^5 = 32
		n := len(cards)
		multiplier := 1
		for i := 0; i < n-1; i++ {
			multiplier *= 2
		}
		return multiplier
	case "throw":
		// 甩牌：优先选择最大的牌型
		// 分析甩牌中包含的牌型，找出最大的

		// 统计每个点数的牌数
		valueCounts := make(map[string]int)
		for _, card := range cards {
			valueCounts[card.Value]++
		}

		// 找出最大的牌型
		maxCardType := "single"

		for _, count := range valueCounts {
			if count >= 3 {
				maxCardType = "triple"
				break // 三张是最大的，找到就停止
			} else if count >= 2 && maxCardType != "triple" {
				maxCardType = "pair"
			}
		}

		// 根据最大牌型计算倍数
		if maxCardType == "triple" {
			return 4 // 2^(3-1) = 4
		} else if maxCardType == "pair" {
			return 2 // 2^(2-1) = 2
		} else {
			return 1 // 2^(1-1) = 1，都是散牌
		}
	default:
		return 1
	}
}

// GetPlayerHand returns a player's hand (only for that player)
func GetPlayerHand(gameID, userID string) (*PlayerHand, error) {
	table, err := GetTableGame(gameID)
	if err != nil {
		return nil, err
	}

	for _, hand := range table.PlayerHands {
		if hand.UserID == userID {
			return hand, nil
		}
	}

	return nil, fmt.Errorf("player not in game")
}

// CreateGame creates a new game
func CreateGame(name, hostID string) (*GameState, error) {
	var id string
	var err error

	// Try to generate a unique ID (retry if collision)
	for i := 0; i < 10; i++ {
		id = generateID()
		query := `INSERT INTO games (id, name, host_id, max_players, status, current_level) VALUES ($1, $2, $3, $4, $5, $6)`
		_, err = db.Exec(query, id, name, hostID, 5, "waiting", "2")
		if err == nil {
			break
		}
		// Check if it's a duplicate error, try again
		if !isDuplicateError(err) {
			return nil, err
		}
	}
	if err != nil {
		return nil, err
	}

	// Add host as first player
	_, err = db.Exec(`INSERT INTO game_players (game_id, user_id, seat_number) VALUES ($1, $2, 1)`, id, hostID)
	if err != nil {
		return nil, err
	}

	// 记录游戏创建日志
	LogGameAction(GameActionLogRequest{
		GameID:     id,
		ActionType: "game_create",
		PlayerSeat: 1,
		PlayerID:   hostID,
		ActionData: map[string]interface{}{
			"game_name":   name,
			"max_players": 5,
			"host_id":     hostID,
		},
		ResultData: map[string]interface{}{
			"game_id": id,
			"status":  "waiting",
		},
	})

	return GetGame(id)
}

// CreateSinglePlayerGame creates a single player game with AI opponents
func CreateSinglePlayerGame(name, hostID string) (*GameState, error) {
	var id string
	var err error

	// Try to generate a unique ID (retry if collision)
	for i := 0; i < 10; i++ {
		id = generateID()
		query := `INSERT INTO games (id, name, host_id, max_players, status, current_level) VALUES ($1, $2, $3, $4, $5, $6)`
		_, err = db.Exec(query, id, name, hostID, 5, "waiting", "2")
		if err == nil {
			break
		}
		// Check if it's a duplicate error, try again
		if !isDuplicateError(err) {
			return nil, err
		}
	}
	if err != nil {
		return nil, err
	}

	// Add human player as seat 1
	_, err = db.Exec(`INSERT INTO game_players (game_id, user_id, seat_number) VALUES ($1, $2, 1)`, id, hostID)
	if err != nil {
		return nil, err
	}

	seats := []map[string]interface{}{
		{"seat": 1, "userId": hostID, "isAI": false},
	}

	// Add AI players for seats 2-5
	// Use simpler AI IDs that are consistent across games
	// Create AI players - reuse existing AI users or create new ones
	for i := 0; i < 4; i++ {
		aiUsername := fmt.Sprintf("AI-%d", i+2)
		seatNumber := i + 2

		// First, check if AI user already exists by username
		var aiID string
		err = db.QueryRow(`SELECT id FROM users WHERE username = $1`, aiUsername).Scan(&aiID)
		if err == sql.ErrNoRows {
			// AI user doesn't exist, create new one
			aiID = fmt.Sprintf("ai-%d", i+2)
			_, err = db.Exec(`INSERT INTO users (id, username, password, level, wins, losses) VALUES ($1, $2, $3, $4, $5, $6)`,
				aiID, aiUsername, "ai", "2", 0, 0)
			if err != nil {
				return nil, fmt.Errorf("failed to create AI user for seat %d: %w", seatNumber, err)
			}
		} else if err != nil {
			return nil, fmt.Errorf("failed to query AI user for seat %d: %w", seatNumber, err)
		}
		// If no error, aiID now contains the existing user's ID

		// Then add to game_players
		_, err = db.Exec(`INSERT INTO game_players (game_id, user_id, seat_number) VALUES ($1, $2, $3)`, id, aiID, seatNumber)
		if err != nil {
			return nil, fmt.Errorf("failed to add AI player to seat %d: %w", seatNumber, err)
		}
		seats = append(seats, map[string]interface{}{"seat": seatNumber, "userId": aiID, "isAI": true})
	}

	LogGameAction(GameActionLogRequest{
		GameID:     id,
		ActionType: "game_create",
		PlayerSeat: 1,
		PlayerID:   hostID,
		ActionData: map[string]interface{}{
			"name":       name,
			"mode":       "singleplayer",
			"hostId":     hostID,
			"maxPlayers": 5,
			"seats":      seats,
		},
		ResultData: map[string]interface{}{"status": "waiting"},
	})

	return GetGame(id)
}

// StartSinglePlayerGame starts a single player game with AI opponents
func StartSinglePlayerGame(gameID, hostID string) (*GameTable, error) {
	game, err := GetGame(gameID)
	if err != nil {
		return nil, err
	}

	if game.HostID != hostID {
		return nil, fmt.Errorf("only host can start the game")
	}

	// Should have exactly 5 players (1 human + 4 AI)
	if len(game.PlayerIDs) != 5 {
		return nil, fmt.Errorf("single player mode requires 5 players")
	}

	if game.Status != "waiting" {
		return nil, fmt.Errorf("game already started")
	}

	// 创建并洗好牌（3副牌），按规则切片：5*31=155 张发给玩家，剩 7 张作底牌
	allCards := createShuffledDeck()
	const cardsToDeal = 5 * 31
	bottomCards := allCards[cardsToDeal : cardsToDeal+7]
	dealingCards := allCards[0:cardsToDeal]

	// 单人模式：玩家1是庄家（起始发牌人）
	startingDealer := 1

	// Initialize game table —— 进入发牌阶段（逐张发牌，1秒1张）
	table := &GameTable{
		GameID:              gameID,
		HostID:              hostID,
		Status:              "dealing", // 发牌阶段
		CurrentLevel:        game.CurrentLevel,
		TrumpSuit:           "",
		HostCalledCard:      nil,
		FriendRevealed:      false,
		BottomCards:         bottomCards,
		CurrentPlayer:       startingDealer,
		TrickLeader:         startingDealer,
		CurrentTrick:        make([]PlayedCard, 0),
		TricksWon:           make([][]Card, 0),
		PlayerHands:         make(map[int]*PlayerHand),
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
		StartingDealerSeat:  startingDealer,
		CurrentCaller:       startingDealer,
		CallPhase:           "dealing", // 发牌中
		CallCountdown:       0,         // 发牌期间不倒计时；发完最后一张才启动 10 秒
		TrumpRank:           game.CurrentLevel,
		FlippedBottomCards:  make([]Card, 0),
		CallRecords:         make([]CallRecord, 0),
		DealingPhase:        "dealing",
		DealtCardCount:      0,
		TotalCardsPerPlayer: 31,
		DealingCards:        dealingCards,
	}

	// 初始化玩家手牌为空（逐张发牌）
	for i, playerID := range game.PlayerIDs {
		seat := i + 1
		table.PlayerHands[seat] = &PlayerHand{
			UserID:     playerID,
			Cards:      make([]Card, 0),
			SeatNumber: seat,
			IsFriend:   false,
			Score:      0,
			Collected:  make([]Card, 0),
		}
	}

	// Store active game
	activeGames[gameID] = table

	// Update game status in database
	UpdateGameStatus(gameID, "playing")

	startSeats := make([]map[string]interface{}, 0, len(table.PlayerHands))
	for seat := 1; seat <= 5; seat++ {
		if h, ok := table.PlayerHands[seat]; ok {
			startSeats = append(startSeats, map[string]interface{}{
				"seat": seat, "userId": h.UserID, "isAI": IsAIUserID(h.UserID),
			})
		}
	}
	LogGameAction(GameActionLogRequest{
		GameID:     gameID,
		ActionType: "game_start",
		PlayerSeat: startingDealer,
		PlayerID:   hostID,
		ActionData: map[string]interface{}{
			"mode":                "singleplayer",
			"currentLevel":        game.CurrentLevel,
			"trumpRank":           table.TrumpRank,
			"startingDealerSeat":  startingDealer,
			"totalCardsPerPlayer": table.TotalCardsPerPlayer,
			"bottomCards":         bottomCards,
			"seats":               startSeats,
		},
		ResultData: map[string]interface{}{
			"status":       table.Status,
			"dealingPhase": table.DealingPhase,
			"deckSize":     len(allCards),
		},
	})

	// 注意：发牌过程中前端会按 1 秒/张轮询 /deal-next，
	// 发牌完成后 DealNextCard 内会自动为人类玩家亮级牌（见 autoCallForHumanIfPossible）。
	return table, nil
}

// findRankCards 查找手牌中的级牌
func findRankCards(cards []Card, rank string) []Card {
	var result []Card
	for _, card := range cards {
		if card.Value == rank {
			result = append(result, card)
		}
	}
	return result
}

// AIPlayTurn makes AI players play. In single-player mode, also plays for the human.
func AIPlayTurn(gameID string) (*GameTable, error) {
	table, err := GetTableGame(gameID)
	if err != nil {
		return nil, err
	}

	if table.Status != "playing" {
		return nil, fmt.Errorf("游戏不在进行中")
	}

	// 只替机器人出牌，轮到真人就停手（单人和多人模式一视同仁）
	maxIterations := 50 // Prevent infinite loop (increased to handle multiple rounds)
	iterations := 0

	for iterations < maxIterations {
		hand, ok := table.PlayerHands[table.CurrentPlayer]
		if !ok {
			return nil, fmt.Errorf("player %d not found", table.CurrentPlayer)
		}
		if !IsAIUserID(hand.UserID) {
			break
		}

		fmt.Printf("DEBUG AIPlayTurn: Player %d (%s) has %d cards\n", table.CurrentPlayer, hand.UserID, len(hand.Cards))

		// Create AI player
		ai := &AIPlayer{
			UserID:     hand.UserID,
			SeatNumber: table.CurrentPlayer,
			Hand:       hand.Cards,
		}

		// Decide which cards to play
		cardIndices := ai.DecidePlay(table)
		fmt.Printf("DEBUG AIPlayTurn: Player %d decided to play cards: %v\n", table.CurrentPlayer, cardIndices)

		// Play the cards
		_, err = PlayCardsGame(gameID, hand.UserID, cardIndices)
		if err != nil {
			fmt.Printf("DEBUG AIPlayTurn: AI %d primary decision rejected: %v. Trying fallback.\n", table.CurrentPlayer, err)
			fallbackIndices, fbErr := fallbackAIPlay(table, ai)
			if fbErr != nil {
				return nil, fmt.Errorf("AI %d play failed: %w (fallback: %v)", table.CurrentPlayer, err, fbErr)
			}
			fmt.Printf("DEBUG AIPlayTurn: Player %d fallback cards: %v\n", table.CurrentPlayer, fallbackIndices)
			_, err = PlayCardsGame(gameID, hand.UserID, fallbackIndices)
			if err != nil {
				return nil, fmt.Errorf("AI %d play failed (after fallback): %w", table.CurrentPlayer, err)
			}
		}

		// Refresh table state
		table, err = GetTableGame(gameID)
		if err != nil {
			return nil, err
		}

		// Check if game finished
		if table.Status != "playing" {
			fmt.Printf("DEBUG AIPlayTurn: Game finished, status=%s\n", table.Status)
			break
		}

		iterations++
	}

	fmt.Printf("DEBUG AIPlayTurn: Completed after %d iterations, currentPlayer=%d\n", iterations, table.CurrentPlayer)
	return table, nil
}

// fallbackAIPlay produces a guaranteed-legal play when the primary AI decision is rejected.
// Strategy:
//   - If leading (no current trick), play the single lowest-value card.
//   - If following, play exactly leadCount cards, prioritising the lead suit
//     (excluding trump-rank cards / jokers, matching the validator's handSuitCards
//     classification), then topping up with other cards from the hand.
func fallbackAIPlay(table *GameTable, ai *AIPlayer) ([]int, error) {
	if len(ai.Hand) == 0 {
		return nil, fmt.Errorf("AI %d has empty hand", ai.SeatNumber)
	}

	if len(table.CurrentTrick) == 0 {
		lowest := 0
		lowestVal := getCardBaseValue(ai.Hand[0])
		for i := 1; i < len(ai.Hand); i++ {
			v := getCardBaseValue(ai.Hand[i])
			if v < lowestVal {
				lowestVal = v
				lowest = i
			}
		}
		return []int{lowest}, nil
	}

	leadSeat := table.CurrentTrick[0].Seat
	var leadCards []Card
	for _, pc := range table.CurrentTrick {
		if pc.Seat == leadSeat {
			leadCards = append(leadCards, pc.Card)
		} else {
			break
		}
	}
	leadCount := len(leadCards)
	if leadCount == 0 {
		return []int{0}, nil
	}
	leadCard := leadCards[0]
	trumpSuit := table.TrumpSuit
	trumpRank := table.TrumpRank

	var strict []int
	for i, card := range ai.Hand {
		if isSameSuitForFollow(card, leadCard, trumpSuit, trumpRank) {
			strict = append(strict, i)
		}
	}

	used := make(map[int]bool)
	var result []int

	sort.Slice(strict, func(i, j int) bool {
		return getCardBaseValue(ai.Hand[strict[i]]) < getCardBaseValue(ai.Hand[strict[j]])
	})

	// 根据领出牌型组织跟牌，遵守牌型规则：
	// - triple：有三张跟三张；没三张有对子跟对子+1散牌；都没有出散牌
	// - pair：有对子跟对子；没有出散牌
	// - 其他（single/tractor/throw）：按价值从低到高凑够数量
	leadCardType := analyzeLeadCardType(leadCards, trumpSuit, trumpRank)

	if leadCardType == "triple" || leadCardType == "pair" {
		// 统计同花色手牌的点数出现次数
		valueIndices := make(map[string][]int)
		for _, idx := range strict {
			v := ai.Hand[idx].Value
			valueIndices[v] = append(valueIndices[v], idx)
		}
		// 找对子/三张
		var tripleVals, pairVals []string
		for v, idxs := range valueIndices {
			if len(idxs) >= 3 {
				tripleVals = append(tripleVals, v)
			} else if len(idxs) >= 2 {
				pairVals = append(pairVals, v)
			}
		}
		if leadCardType == "triple" && len(tripleVals) > 0 {
			// 有三张：出三张
			idxs := valueIndices[tripleVals[0]]
			for _, idx := range idxs[:3] {
				result = append(result, idx)
				used[idx] = true
			}
		} else if len(pairVals) > 0 || (leadCardType == "triple" && len(tripleVals) > 0) {
			// 有对子（triple 时没三张但有对子，或 pair 时有对子）：出对子
			var pairIdxs []int
			if leadCardType == "triple" && len(tripleVals) > 0 {
				pairIdxs = valueIndices[tripleVals[0]][:2]
			} else {
				pairIdxs = valueIndices[pairVals[0]][:2]
			}
			for _, idx := range pairIdxs {
				result = append(result, idx)
				used[idx] = true
			}
			// triple 还需要补1张散牌
			if leadCardType == "triple" {
				for _, idx := range strict {
					if !used[idx] {
						result = append(result, idx)
						used[idx] = true
						break
					}
				}
			}
		}
		// 没有对子/三张：result 保持为空，下面统一用散牌凑
	}

	// 用散牌凑够 leadCount（处理 single/tractor/throw，以及上面没凑够的情况）
	for _, idx := range strict {
		if len(result) >= leadCount {
			break
		}
		if !used[idx] {
			result = append(result, idx)
			used[idx] = true
		}
	}

	if len(result) < leadCount {
		var rest []int
		for i := range ai.Hand {
			if !used[i] {
				rest = append(rest, i)
			}
		}
		sort.Slice(rest, func(i, j int) bool {
			return getCardBaseValue(ai.Hand[rest[i]]) < getCardBaseValue(ai.Hand[rest[j]])
		})
		for _, idx := range rest {
			if len(result) >= leadCount {
				break
			}
			result = append(result, idx)
		}
	}

	if len(result) != leadCount {
		return nil, fmt.Errorf("fallback could not assemble %d cards (got %d)", leadCount, len(result))
	}
	return result, nil
}

// AICallFriendCard decides which card to call as friend
func AICallFriendCard(cards []Card) (string, string) {
	if len(cards) == 0 {
		return "spades", "A"
	}

	// Strategy: Call Ace of longest suit or a suit we have multiple cards of
	suitCounts := make(map[string]int)
	for _, card := range cards {
		suitCounts[card.Suit]++
	}

	longestSuit := ""
	maxCount := 0
	for suit, count := range suitCounts {
		if count > maxCount {
			maxCount = count
			longestSuit = suit
		}
	}

	// Check if we have the Ace
	for _, card := range cards {
		if card.Suit == longestSuit && card.Value == "A" {
			return longestSuit, "A" // We have it, 1v4 mode
		}
	}

	return longestSuit, "A"
}

// GetGame retrieves a game by ID
func GetGame(id string) (*GameState, error) {
	query := `SELECT id, name, host_id, max_players, status, current_level, created_at, updated_at FROM games WHERE id = $1`

	game := &GameState{}
	err := db.QueryRow(query, id).Scan(
		&game.ID, &game.Name, &game.HostID, &game.MaxPlayers,
		&game.Status, &game.CurrentLevel, &game.CreatedAt, &game.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrGameNotFound
	}
	if err != nil {
		return nil, err
	}

	// Load players
	game.PlayerIDs, err = getGamePlayers(id)
	if err != nil {
		return nil, err
	}

	return game, nil
}

// getGamePlayers gets all player IDs for a game
func getGamePlayers(gameID string) ([]string, error) {
	query := `SELECT user_id FROM game_players WHERE game_id = $1 ORDER BY seat_number`

	rows, err := db.Query(query, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var playerIDs []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		playerIDs = append(playerIDs, userID)
	}

	return playerIDs, rows.Err()
}

// JoinGame adds a player to a game
func JoinGame(gameID, playerID string) error {
	// Check if game exists and get current player count
	game, err := GetGame(gameID)
	if err != nil {
		return err
	}

	if len(game.PlayerIDs) >= game.MaxPlayers {
		return ErrGameFull
	}

	// Check if player already in game
	for _, id := range game.PlayerIDs {
		if id == playerID {
			return nil // Already in game
		}
	}

	// Get next seat number
	nextSeat := len(game.PlayerIDs) + 1

	// Add player to game
	query := `INSERT INTO game_players (game_id, user_id, seat_number) VALUES ($1, $2, $3)`
	_, err = db.Exec(query, gameID, playerID, nextSeat)

	if err == nil {
		// 记录玩家加入日志
		LogGameAction(GameActionLogRequest{
			GameID:     gameID,
			ActionType: "player_join",
			PlayerSeat: nextSeat,
			PlayerID:   playerID,
			ActionData: map[string]interface{}{
				"seat_number":   nextSeat,
				"current_count": nextSeat,
			},
			ResultData: map[string]interface{}{
				"status": "success",
			},
		})
	}

	return err
}

// ListGames returns all active games
func ListGames() ([]*GameState, error) {
	query := `SELECT id, name, host_id, max_players, status, current_level, created_at, updated_at FROM games WHERE status != 'finished' ORDER BY created_at DESC`

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var games []*GameState
	for rows.Next() {
		game := &GameState{}
		err := rows.Scan(
			&game.ID, &game.Name, &game.HostID, &game.MaxPlayers,
			&game.Status, &game.CurrentLevel, &game.CreatedAt, &game.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		// Load players
		game.PlayerIDs, err = getGamePlayers(game.ID)
		if err != nil {
			return nil, err
		}

		games = append(games, game)
	}

	return games, rows.Err()
}

// UpdateGameStatus updates the status of a game
func UpdateGameStatus(gameID, status string) error {
	query := `UPDATE games SET status = $1 WHERE id = $2`
	_, err := db.Exec(query, status, gameID)
	return err
}

// UpdateGameCurrentLevel updates the current level of a game in the database
func UpdateGameCurrentLevel(gameID, level string) error {
	query := `UPDATE games SET current_level = $1 WHERE id = $2`
	_, err := db.Exec(query, level, gameID)
	return err
}

// NextRound resets the table for a new round after a game has ended
// It preserves player levels (already updated by RecordGameResult) and
// resets all game state for the new round
func NextRound(gameID string) (*GameTable, error) {
	lock := getGameLock(gameID)
	lock.Lock()
	defer lock.Unlock()

	game, err := GetGame(gameID)
	if err != nil {
		return nil, err
	}

	table, ok := activeGames[gameID]
	if !ok {
		return nil, fmt.Errorf("game table not found")
	}

	if table.Status != "finished" {
		return nil, fmt.Errorf("game is not finished, cannot start next round")
	}

	// Get the new current level: use the winning team's new level (already updated in DB by RecordGameResult).
	// Since the winning team always advances, their level is the highest among all players,
	// so taking the max correctly reflects the next round's trump rank.
	newLevel := "2"
	maxLevelIndex := -1
	levels := []string{"2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K", "A"}
	for _, playerID := range game.PlayerIDs {
		user, err := GetUserByID(playerID)
		if err != nil {
			continue
		}
		for i, lvl := range levels {
			if lvl == user.Level && i > maxLevelIndex {
				maxLevelIndex = i
				newLevel = user.Level
			}
		}
	}

	// Determine starting dealer based on previous round result
	startingDealer := determineStartingDealer(table.PreviousResult, table.DealerSeat)

	// Update game's current level in database
	if err := UpdateGameCurrentLevel(gameID, newLevel); err != nil {
		return nil, fmt.Errorf("failed to update game level: %v", err)
	}

	// Create new shuffled deck
	allCards := createShuffledDeck()

	// Determine number of players
	numPlayers := len(game.PlayerIDs)
	if numPlayers < 3 {
		numPlayers = 3
	}
	cardsToDeal := numPlayers * 31
	if cardsToDeal > 155 {
		cardsToDeal = 155
	}
	bottomCards := allCards[cardsToDeal : cardsToDeal+7]
	dealingCards := allCards[0:cardsToDeal]

	// Reset table for new round
	table.Status = "dealing"
	table.CurrentLevel = newLevel
	table.TrumpSuit = ""
	table.TrumpRank = newLevel
	table.HostCalledCard = nil
	table.FriendRevealed = false
	table.FriendSeat = 0
	table.IsSoloMode = false
	table.BottomCards = bottomCards
	table.CurrentPlayer = startingDealer
	table.TrickLeader = startingDealer
	table.CurrentTrick = make([]PlayedCard, 0)
	table.LastCompletedTrick = make([]PlayedCard, 0)
	table.TricksWon = make([][]Card, 0)
	table.CurrentCaller = startingDealer
	table.CallPhase = "dealing"
	table.CallCountdown = 0 // 发牌期间不倒计时；发完最后一张才启动 10 秒
	table.CallRecords = make([]CallRecord, 0)
	table.PassedSeats = make([]int, 0)
	table.FlippedBottomCards = make([]Card, 0)
	table.StartingDealerSeat = startingDealer
	table.DealingPhase = "dealing"
	table.DealtCardCount = 0
	table.DealingCards = dealingCards
	table.TotalCardsPerPlayer = 31
	table.ThrowBlocker = 0
	table.ThrowBlockerCard = ""
	table.TotalPoints = 0
	table.RoundResults = nil
	table.NextRoundCountdownStart = ""
	table.LastPlay = nil

	// Reset each player's hand
	for _, hand := range table.PlayerHands {
		hand.Cards = make([]Card, 0)
		hand.IsFriend = false
		hand.HasCalled = false
		hand.Score = 0
		hand.Collected = make([]Card, 0)
		// Update level from database
		if u, err := GetUserByID(hand.UserID); err == nil {
			hand.Level = u.Level
		}
	}

	table.UpdatedAt = time.Now()

	// Log next round
	LogGameAction(GameActionLogRequest{
		GameID:     gameID,
		ActionType: "next_round",
		PlayerSeat: 0,
		PlayerID:   "",
		ActionData: map[string]interface{}{
			"new_level":       newLevel,
			"starting_dealer": startingDealer,
			"previous_result": table.PreviousResult,
		},
	})

	return table, nil
}

// GetGamePlayersWithInfo returns detailed player information for a game
func GetGamePlayersWithInfo(gameID string) ([]*PlayerInfo, error) {
	query := `
		SELECT u.id, u.username, u.level, u.wins, u.losses, gp.seat_number
		FROM game_players gp
		JOIN users u ON gp.user_id = u.id
		WHERE gp.game_id = $1
		ORDER BY gp.seat_number
	`

	rows, err := db.Query(query, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var players []*PlayerInfo
	for rows.Next() {
		p := &PlayerInfo{}
		err := rows.Scan(&p.ID, &p.Username, &p.Level, &p.Wins, &p.Losses, &p.SeatNumber)
		if err != nil {
			return nil, err
		}
		players = append(players, p)
	}

	return players, rows.Err()
}

// PlayerInfo represents player information with seat number
type PlayerInfo struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	Level      string `json:"level"`
	Wins       int    `json:"wins"`
	Losses     int    `json:"losses"`
	SeatNumber int    `json:"seatNumber"`
}

// DealCards deals cards for a 5-player, 3-deck game
// Each player gets 31 cards, 7 cards go to the bottom
func DealCards(playerCount int) ([][]Card, []Card) {
	// Create 3 decks of cards (162 cards total)
	var allCards []Card
	suits := []string{"hearts", "diamonds", "clubs", "spades"}
	values := []string{"2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K", "A"}

	for deck := 0; deck < 3; deck++ {
		for _, suit := range suits {
			for _, value := range values {
				allCards = append(allCards, Card{
					Suit:  suit,
					Value: value,
					Type:  "normal",
				})
			}
		}
		// Add jokers for each deck
		allCards = append(allCards, Card{Suit: "joker", Value: "small", Type: "joker"})
		allCards = append(allCards, Card{Suit: "joker", Value: "big", Type: "joker"})
	}

	// Proper shuffle using rand
	rand.Seed(time.Now().UnixNano())
	for i := len(allCards) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		allCards[i], allCards[j] = allCards[j], allCards[i]
	}

	// Deal cards
	hands := make([][]Card, playerCount)
	cardsPerPlayer := 31

	for i := 0; i < playerCount; i++ {
		hands[i] = allCards[i*cardsPerPlayer : (i+1)*cardsPerPlayer]
		// 排序：按花色从左到右，从小到大
		hands[i] = sortCards(hands[i])
	}

	// 底牌也排序
	bottomCards := allCards[playerCount*cardsPerPlayer:]
	bottomCards = sortCards(bottomCards)

	return hands, bottomCards
}

// sortCards sorts cards by suit then by value
// 花色顺序: hearts < diamonds < clubs < spades < joker
// 点数顺序: 2 < 3 < 4 < ... < A < small < big
func sortCards(cards []Card) []Card {
	suitOrder := map[string]int{
		"hearts":   0,
		"diamonds": 1,
		"clubs":    2,
		"spades":   3,
		"joker":    4,
	}
	valueOrder := map[string]int{
		"2":     0,
		"3":     1,
		"4":     2,
		"5":     3,
		"6":     4,
		"7":     5,
		"8":     6,
		"9":     7,
		"10":    8,
		"J":     9,
		"Q":     10,
		"K":     11,
		"A":     12,
		"small": 13,
		"big":   14,
	}

	sorted := make([]Card, len(cards))
	copy(sorted, cards)

	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			si := suitOrder[sorted[i].Suit]
			sj := suitOrder[sorted[j].Suit]
			if si > sj {
				sorted[i], sorted[j] = sorted[j], sorted[i]
				continue
			}
			if si < sj {
				continue
			}
			// 同花色，按点数排序
			vi := valueOrder[sorted[i].Value]
			vj := valueOrder[sorted[j].Value]
			if vi > vj {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	return sorted
}

// CalculateLevelUp determines how many levels to advance based on score
// 规则：闲家得分决定升级，闲家永不降级
// 正常局升级表（庄家找到盟友，2打3）
// | 闲家得分范围 | 结果 | 庄家方升级 | 闲家升级 |
// | 0 - 119 分  | 庄胜 | 升 1 级 | 不升级 |
// | 120 - 179 分 | 反超 | 不升级 | 每人升 1 级 |
// | 180 - 239 分 | 大胜 | 不升级 | 每人升 2 级 |
// | 240 - 299 分 | 完胜 | 不升级 | 每人升 3 级 |
// | 300 分 | 满光 | 不升级 | 每人升 4 级 |
//
// 独打局升级表（庄家 1 打 4）
// | 闲家得分范围 | 结果 | 庄家升级 | 闲家升级 |
// | 0 - 119 分  | 庄胜 | 升 1 级 | 不升级 |
// | 120 - 179 分 | 反超 | 不升级 | 每人升 1 级 |
// | 180 分及以上 | 大胜 | 不升级 | 每人升 2 级 |
func CalculateLevelUp(score int, isSolo bool, winnerIsDefender bool) int {
	if isSolo {
		// 独打局（庄家 1 打 4）
		if winnerIsDefender {
			// 庄家（防守方）获胜：闲家得分 0-119，庄家升 1 级
			return 1
		} else {
			// 闲家获胜
			if score >= 180 {
				return 2 // 大胜，闲家每人升 2 级
			}
			return 1 // 反超（120-179分），闲家每人升 1 级
		}
	} else {
		// 正常局（庄家找到盟友，2 打 3）
		if winnerIsDefender {
			// 庄家方获胜：闲家得分 0-119，庄家升 1 级
			return 1
		} else {
			// 闲家获胜
			if score >= 300 {
				return 4 // 满光，闲家每人升 4 级
			} else if score >= 240 {
				return 3 // 完胜，闲家每人升 3 级
			} else if score >= 180 {
				return 2 // 大胜，闲家每人升 2 级
			}
			return 1 // 反超（120-179分），闲家每人升 1 级
		}
	}
}

// RecordGameResult records the result of a game for all players
// keepPlaying: if true, keeps game status as "playing" (multi-round mode); if false, marks as "finished"
func RecordGameResult(gameID string, results []GameResult, dealerSeat int, winnerTeam string, friendSeat int, isSolo bool, keepPlaying bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO game_records (game_id, user_id, old_level, new_level, is_winner, score)
		VALUES ($1, $2, $3, $4, $5, $6)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, r := range results {
		_, err = stmt.Exec(gameID, r.UserID, r.OldLevel, r.NewLevel, r.IsWinner, r.Score)
		if err != nil {
			return err
		}

		// Update user level
		if err := UpdateUserLevel(r.UserID, r.NewLevel); err != nil {
			return err
		}

		// Update win/loss count
		if r.IsWinner {
			if err := IncrementUserWins(r.UserID); err != nil {
				return err
			}
		} else {
			if err := IncrementUserLosses(r.UserID); err != nil {
				return err
			}
		}
	}

	// 记录游戏结束日志
	levelChanges := make([]map[string]interface{}, 0, len(results))
	for _, r := range results {
		levelChanges = append(levelChanges, map[string]interface{}{
			"user_id":   r.UserID,
			"old_level": r.OldLevel,
			"new_level": r.NewLevel,
			"is_winner": r.IsWinner,
		})
	}

	LogGameAction(GameActionLogRequest{
		GameID:     gameID,
		ActionType: "game_end",
		PlayerSeat: 0,
		PlayerID:   "",
		ActionData: map[string]interface{}{
			"results": results,
		},
		ResultData: map[string]interface{}{
			"level_changes": levelChanges,
			"game_status":   "finished",
		},
	})

	// 保存上一局结果到数据库，供下一局使用
	// keepPlaying: true 时保持 "playing" 状态（多局模式），false 时设为 "finished"
	newStatus := "finished"
	if keepPlaying {
		newStatus = "playing"
	}
	if _, err := tx.Exec(`
		UPDATE games
		SET status = $6,
		    previous_dealer_seat = $2,
		    previous_winner_team = $3,
		    previous_friend_seat = $4,
		    previous_is_solo = $5
		WHERE id = $1
	`, gameID, dealerSeat, winnerTeam, friendSeat, isSolo, newStatus); err != nil {
		return err
	}

	return tx.Commit()
}

// determineStartingDealer 根据上一局结果确定起始发牌人
// 规则2.2：
// - 庄家赢（1V4）：庄家当起始发牌人
// - 庄家赢（2V3）：庄家的朋友当起始发牌人
// - 庄家输：逆时针最靠近庄家的玩家当起始发牌人
func determineStartingDealer(prevResult *PreviousGameResult, prevDealerSeat int) int {
	if prevResult == nil {
		return 1 // 默认值
	}

	// 检查上一局庄家是否获胜
	if prevResult.WinnerTeam == "host" {
		// 庄家方获胜
		if prevResult.IsSoloMode {
			// 1V4模式：庄家当起始发牌人
			return prevDealerSeat
		} else {
			// 2V3模式：庄家的朋友当起始发牌人
			return prevResult.FriendSeat
		}
	} else {
		// 庄家输：逆时针最靠近庄家的玩家
		// 逆时针顺序：1→5→4→3→2→1
		// 例如庄家在1号位，逆时针最靠近的是5号位
		return ((prevDealerSeat - 1 - 1 + 5) % 5) + 1
	}
}

// getPreviousGameResult 从数据库获取上一局的结果
func getPreviousGameResult(gameID string) (*PreviousGameResult, error) {
	var winnerTeam string
	var dealerSeat, friendSeat int
	var isSolo bool

	err := db.QueryRow(`
		SELECT previous_winner_team, previous_dealer_seat,
		       previous_friend_seat, previous_is_solo
		FROM games WHERE id = $1
	`, gameID).Scan(&winnerTeam, &dealerSeat, &friendSeat, &isSolo)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no previous game result found")
	}
	if err != nil {
		return nil, err
	}

	// 如果没有上一局结果（全是默认值）
	if winnerTeam == "" && dealerSeat == 0 {
		return nil, fmt.Errorf("no valid previous game result")
	}

	return &PreviousGameResult{
		WinnerTeam: winnerTeam,
		DealerSeat: dealerSeat,
		FriendSeat: friendSeat,
		IsSoloMode: isSolo,
	}, nil
}

// GameResult represents the result for a single player
type GameResult struct {
	UserID   string `json:"user_id"`
	OldLevel string `json:"old_level"`
	NewLevel string `json:"new_level"`
	IsWinner bool   `json:"is_winner"`
	Score    int    `json:"score"`
}

// PlayCardsGame plays multiple cards from a player's hand
// isTrickComplete checks if a trick is complete.
func isTrickComplete(table *GameTable) bool {
	if len(table.CurrentTrick) == 0 {
		return false
	}
	seatsActed := make(map[int]bool)
	for _, pc := range table.CurrentTrick {
		seatsActed[pc.Seat] = true
	}
	totalPlayers := len(table.PlayerHands)
	passCount := table.TrickPassCount
	return len(seatsActed)+passCount == totalPlayers && seatsActed[table.TrickLeader]
}

// advanceCallingPlayer advances CurrentPlayer to the next player who hasn't acted
// in the calling phase (counter-clockwise: 1→5→4→3→2→1)
func advanceCallingPlayer(table *GameTable) {
	totalPlayers := len(table.PlayerHands)
	acted := make(map[int]bool)
	for _, r := range table.CallRecords {
		acted[r.Seat] = true
	}
	for _, s := range table.PassedSeats {
		acted[s] = true
	}
	current := table.CurrentPlayer
	for i := 0; i < totalPlayers; i++ {
		next := ((current - 2 + 5) % 5) + 1
		if !acted[next] {
			table.CurrentPlayer = next
			return
		}
		current = next
	}
}

// PassTurn handles a player passing their turn (不出)
func PassTurn(gameID, userID string) (*PlayResult, error) {
	lock := getGameLock(gameID)
	lock.Lock()
	defer lock.Unlock()

	table, err := GetTableGame(gameID)
	if err != nil {
		return nil, err
	}

	if table.Status != "playing" {
		return nil, fmt.Errorf("游戏不在进行中")
	}

	// Find player's seat
	var playerSeat int
	var hand *PlayerHand
	for seat, h := range table.PlayerHands {
		if h.UserID == userID {
			playerSeat = seat
			hand = h
			break
		}
	}

	if hand == nil {
		return nil, fmt.Errorf("player not in game")
	}

	if playerSeat != table.CurrentPlayer {
		return nil, fmt.Errorf("not your turn")
	}

	// 不能在首轮出牌时选择不出
	if len(table.CurrentTrick) == 0 {
		return nil, fmt.Errorf("cannot pass when leading")
	}

	// Move to next player (counter-clockwise: 1→5→4→3→2→1)
	nextPlayer := ((playerSeat - 2 + 5) % 5) + 1
	table.CurrentPlayer = nextPlayer

	result := &PlayResult{
		Success:    true,
		Message:    "Pass",
		NextPlayer: nextPlayer,
	}

	LogGameAction(GameActionLogRequest{
		GameID:     gameID,
		ActionType: "pass_turn",
		PlayerSeat: playerSeat,
		PlayerID:   userID,
		ActionData: map[string]interface{}{
			"handCount":  len(hand.Cards),
			"trickSoFar": len(table.CurrentTrick),
		},
		ResultData: map[string]interface{}{
			"nextPlayer": nextPlayer,
		},
	})

	// Count this pass toward the trick completion check
	table.TrickPassCount++

	fmt.Printf("[DEBUG] PassTurn: playerSeat=%d, CurrentPlayer=%d, trickLen=%d, isComplete=%v\n",
		playerSeat, table.CurrentPlayer, len(table.CurrentTrick), isTrickComplete(table))

	// Check if trick is complete (all 5 players have played or passed)
	if isTrickComplete(table) {
		winner := determineTrickWinner(table.CurrentTrick, table.TrumpSuit, table.TrumpRank)
		result.TrickComplete = true
		result.TrickWinner = winner

		// 当轮结束，清除甩牌失败高亮
		table.ThrowBlocker = 0
		table.ThrowBlockerCard = ""

		// Collect scoring cards
		var collectedCards []Card
		for _, pc := range table.CurrentTrick {
			if isScoringCard(pc.Card) {
				collectedCards = append(collectedCards, pc.Card)
			}
		}

		// Winner gets the cards
		if winnerHand, ok := table.PlayerHands[winner]; ok {
			winnerHand.Collected = append(winnerHand.Collected, collectedCards...)
		}

		// Clear trick and set winner as next leader
		// Save current trick to last completed trick before clearing
		table.LastCompletedTrick = make([]PlayedCard, len(table.CurrentTrick))
		copy(table.LastCompletedTrick, table.CurrentTrick)
		table.CurrentTrick = make([]PlayedCard, 0)
		table.CurrentPlayer = winner
		table.TrickLeader = winner
		table.TrickPassCount = 0
		result.NextPlayer = winner
	}

	table.LastPlay = result
	table.UpdatedAt = time.Now()

	// Save the updated table state
	activeGames[gameID] = table

	return result, nil
}

func PlayCardsGame(gameID, userID string, cardIndices []int) (*PlayResult, error) {
	lock := getGameLock(gameID)
	lock.Lock()
	defer lock.Unlock()

	table, err := GetTableGame(gameID)
	if err != nil {
		return nil, err
	}

	if table.Status != "playing" {
		return nil, fmt.Errorf("游戏不在进行中")
	}

	// Find player's seat
	var playerSeat int
	var hand *PlayerHand
	for seat, h := range table.PlayerHands {
		if h.UserID == userID {
			playerSeat = seat
			hand = h
			break
		}
	}

	if hand == nil {
		return nil, fmt.Errorf("player not in game")
	}

	if playerSeat != table.CurrentPlayer {
		return nil, fmt.Errorf("not your turn")
	}

	// Validate card indices
	if len(cardIndices) == 0 {
		return nil, fmt.Errorf("no cards selected")
	}

	// Validate all card indices
	for _, idx := range cardIndices {
		if idx < 0 || idx >= len(hand.Cards) {
			return nil, fmt.Errorf("invalid card index: %d", idx)
		}
	}

	// Get the cards to be played
	var cardsToPlay []Card
	for _, idx := range cardIndices {
		cardsToPlay = append(cardsToPlay, hand.Cards[idx])
	}

	// Check if this is a throw (甩牌) - leading with multiple cards of same suit
	isLead := len(table.CurrentTrick) == 0
	if isLead && len(cardsToPlay) >= 2 {
		// Check for throw cards validation
		attemptedThrow := append([]Card(nil), cardsToPlay...)
		throwResult := ValidateThrowCards(cardsToPlay, table, playerSeat)

		if !throwResult.IsValid && len(throwResult.ActualPlay) < len(cardsToPlay) {
			// 甩牌失败，记录让甩牌失败的玩家（用于高亮显示）
			table.ThrowBlocker = throwResult.BlockerSeat
			table.ThrowBlockerCard = throwResult.BlockerCard

			// 找到要出的牌在原手牌中的索引
			actualCardIndices := make([]int, 0, len(throwResult.ActualPlay))
			usedIndices := make(map[int]bool)
			for _, actualCard := range throwResult.ActualPlay {
				for i, idx := range cardIndices {
					if !usedIndices[i] {
						originalCard := hand.Cards[idx]
						if originalCard.Suit == actualCard.Suit && originalCard.Value == actualCard.Value {
							actualCardIndices = append(actualCardIndices, idx)
							usedIndices[i] = true
							break
						}
					}
				}
			}
			cardIndices = actualCardIndices
			cardsToPlay = throwResult.ActualPlay
			fmt.Printf("甩牌失败: %s，只出最小牌\n", throwResult.Reason)

			LogGameAction(GameActionLogRequest{
				GameID:     gameID,
				ActionType: "throw_rejected",
				PlayerSeat: playerSeat,
				PlayerID:   userID,
				ActionData: map[string]interface{}{
					"attempted": attemptedThrow,
					"kept":      throwResult.ActualPlay,
				},
				ResultData: map[string]interface{}{
					"reason":       throwResult.Reason,
					"blockerSeat":  throwResult.BlockerSeat,
					"blockerCard":  throwResult.BlockerCard,
					"attemptCount": len(attemptedThrow),
					"keptCount":    len(throwResult.ActualPlay),
				},
			})
		} else if throwResult.IsValid {
			LogGameAction(GameActionLogRequest{
				GameID:     gameID,
				ActionType: "throw_accepted",
				PlayerSeat: playerSeat,
				PlayerID:   userID,
				ActionData: map[string]interface{}{
					"cards": cardsToPlay,
				},
				ResultData: map[string]interface{}{
					"count": len(cardsToPlay),
				},
			})
		}
	}

	// Validate the play (must be valid combination)
	fmt.Printf("DEBUG: Validating %d cards: %+v\n", len(cardsToPlay), cardsToPlay)
	if err := validateCardPlay(cardsToPlay, table, hand); err != nil {
		fmt.Printf("DEBUG: Validation failed: %v\n", err)
		return nil, err
	}

	// Check for friend reveal with played cards
	// 追踪打出次数，当打出第N张时识别盟友
	if !table.FriendRevealed && table.HostCalledCard != nil && len(cardsToPlay) > 0 {
		// 先统计这次打出了多少张朋友牌
		friendCardCount := 0
		for _, card := range cardsToPlay {
			if card.Suit == table.HostCalledCard.Suit && card.Value == table.HostCalledCard.Value {
				friendCardCount++
			}
		}

		// 如果打出了朋友牌，更新计数并检查是否识别盟友
		if friendCardCount > 0 {
			// 计数器增加
			table.HostCalledCard.Count += friendCardCount

			// 检查是否达到或超过指定的position
			if table.HostCalledCard.Count >= table.HostCalledCard.Position {
				// 第N张被打出，识别盟友
				table.FriendRevealed = true
				table.FriendSeat = playerSeat
				hand.IsFriend = true

				LogGameAction(GameActionLogRequest{
					GameID:     gameID,
					ActionType: "friend_revealed",
					PlayerSeat: playerSeat,
					PlayerID:   userID,
					ActionData: map[string]interface{}{
						"calledCard": fmt.Sprintf("%s%s", table.HostCalledCard.Suit, table.HostCalledCard.Value),
						"position":   table.HostCalledCard.Position,
						"playedNth":  table.HostCalledCard.Count,
					},
					ResultData: map[string]interface{}{
						"friendSeat": playerSeat,
						"dealerSeat": table.DealerSeat,
					},
				})
			}
		}
	}

	// Remove cards from hand (remove in reverse order to preserve indices)
	sort.Slice(cardIndices, func(i, j int) bool { return cardIndices[i] > cardIndices[j] })
	fmt.Printf("DEBUG: Before removing cards, hand has %d cards\n", len(hand.Cards))
	for _, idx := range cardIndices {
		hand.Cards = append(hand.Cards[:idx], hand.Cards[idx+1:]...)
	}
	// Update the hand in the table to ensure the reference is correct
	table.PlayerHands[playerSeat].Cards = hand.Cards
	fmt.Printf("DEBUG: After removing %d cards, hand has %d cards\n", len(cardIndices), len(hand.Cards))

	// Add all played cards to current trick
	isLead = len(table.CurrentTrick) == 0
	if isLead {
		table.TrickLeader = playerSeat
	}

	for _, card := range cardsToPlay {
		table.CurrentTrick = append(table.CurrentTrick, PlayedCard{
			Card:      card,
			Seat:      playerSeat,
			IsLead:    isLead,
			Timestamp: time.Now().UnixNano(),
		})
	}

	table.UpdatedAt = time.Now()

	// 判断牌型用于日志记录
	playType := "single"
	if len(cardsToPlay) == 2 && cardsToPlay[0].Value == cardsToPlay[1].Value {
		playType = "pair"
	} else if len(cardsToPlay) == 3 && cardsToPlay[0].Value == cardsToPlay[1].Value && cardsToPlay[1].Value == cardsToPlay[2].Value {
		playType = "triple"
	} else if len(cardsToPlay) >= 4 && isTractorWithContext(cardsToPlay, table.TrumpSuit, table.TrumpRank) {
		playType = "tractor"
	} else if len(cardsToPlay) > 1 {
		playType = "throw"
	}

	// 记录出牌日志
	LogGameAction(GameActionLogRequest{
		GameID:     gameID,
		ActionType: "play_cards",
		PlayerSeat: playerSeat,
		PlayerID:   userID,
		ActionData: map[string]interface{}{
			"card_indices": cardIndices,
			"cards":        cardsToPlay,
			"is_lead":      isLead,
			"play_type":    playType,
		},
		ResultData: map[string]interface{}{
			"success": true,
		},
	})

	result := &PlayResult{
		Success: true,
		Message: fmt.Sprintf("Played %d cards", len(cardsToPlay)),
	}

	// Update next player if trick not yet complete
	if !isTrickComplete(table) {
		result.NextPlayer = ((playerSeat - 2 + 5) % 5) + 1
		table.CurrentPlayer = result.NextPlayer
		fmt.Printf("[DEBUG] seat%d played %v, trick NOT complete, nextPlayer=%d, trickLen=%d\n",
			playerSeat, cardsToPlay, result.NextPlayer, len(table.CurrentTrick))
	} else {
		fmt.Printf("[DEBUG] seat%d played %v, trick IS complete!\n", playerSeat, cardsToPlay)
	}

	// Check if trick is complete (all 5 players have played)
	if isTrickComplete(table) {
		winner := determineTrickWinner(table.CurrentTrick, table.TrumpSuit, table.TrumpRank)
		result.TrickComplete = true
		result.TrickWinner = winner

		// 当轮结束，清除甩牌失败高亮
		table.ThrowBlocker = 0
		table.ThrowBlockerCard = ""

		// Collect scoring cards
		var collectedCards []Card
		pointsCollected := 0
		for _, pc := range table.CurrentTrick {
			if isScoringCard(pc.Card) {
				collectedCards = append(collectedCards, pc.Card)
				pointsCollected += getCardPoints(pc.Card)
			}
		}

		// Winner gets the cards
		if winnerHand, ok := table.PlayerHands[winner]; ok {
			winnerHand.Collected = append(winnerHand.Collected, collectedCards...)
			// 更新分数
			winnerHand.Score += pointsCollected
		}

		// Store all played cards in tricks won
		var trickCards []Card
		for _, pc := range table.CurrentTrick {
			trickCards = append(trickCards, pc.Card)
		}
		table.TricksWon = append(table.TricksWon, trickCards)

		// 判断最后一圈的牌型（用于抠底计算）
		// 获取赢家出的牌
		var winnerCards []Card
		for _, pc := range table.CurrentTrick {
			if pc.Seat == winner {
				winnerCards = append(winnerCards, pc.Card)
			}
		}
		lastTrickCardType := determineCardType(winnerCards, table.TrumpSuit, table.TrumpRank)

		// 记录回合结束日志
		LogGameAction(GameActionLogRequest{
			GameID:     gameID,
			ActionType: "trick_complete",
			PlayerSeat: winner,
			PlayerID:   table.PlayerHands[winner].UserID,
			ActionData: map[string]interface{}{
				"trick_number": len(table.TricksWon),
				"trick_cards":  trickCards,
			},
			ResultData: map[string]interface{}{
				"winner_seat":      winner,
				"points_collected": pointsCollected,
				"scoring_cards":    collectedCards,
				"next_leader":      winner,
			},
		})

		// Clear trick and set winner as next leader
		// Save current trick to last completed trick before clearing
		table.LastCompletedTrick = make([]PlayedCard, len(table.CurrentTrick))
		copy(table.LastCompletedTrick, table.CurrentTrick)
		table.CurrentTrick = make([]PlayedCard, 0)
		table.CurrentPlayer = winner
		table.TrickLeader = winner
		table.TrickPassCount = 0

		// Check if game ended (all cards played)
		allCardsPlayed := true
		for _, hand := range table.PlayerHands {
			if len(hand.Cards) > 0 {
				allCardsPlayed = false
				break
			}
		}

		if allCardsPlayed {
			// Game ended - calculate final scores and results
			result.GameEnded = true
			table.Status = "finished"
			// 注意：不调用 UpdateGameStatus(gameID, "finished")，保持 "playing" 状态以便新一局继续

			// Calculate total points collected by non-host team
			totalPoints := 0
			for seat, hand := range table.PlayerHands {
				// If not host or friend, count points
				if seat != table.DealerSeat && (!table.FriendRevealed || seat != table.FriendSeat) {
					for _, card := range hand.Collected {
						totalPoints += getCardPoints(card)
					}
				}
			}

			// Add bottom cards to score if non-host team won last trick (抠底)
			bottomKicked := false
			bottomMultiplier := 0
			bottomPoints := 0
			if winner != table.DealerSeat && (!table.FriendRevealed || winner != table.FriendSeat) {
				// Non-host team won last trick - 根据抠底牌型计算倍数
				if table.BottomCards != nil {
					bottomKicked = true
					bottomMultiplier = calculateBottomCardsMultiplier(winnerCards, table)
					for _, bottomCard := range table.BottomCards {
						bottomPoints += getCardPoints(bottomCard) * bottomMultiplier
					}
					totalPoints += bottomPoints
				}
			}
			LogGameAction(GameActionLogRequest{
				GameID:     gameID,
				ActionType: "bottom_kick",
				PlayerSeat: winner,
				PlayerID:   "",
				ActionData: map[string]interface{}{
					"kicked":         bottomKicked,
					"winnerSeat":     winner,
					"dealerSeat":     table.DealerSeat,
					"friendSeat":     table.FriendSeat,
					"lastTrickType":  lastTrickCardType,
					"lastTrickCards": winnerCards,
					"bottomCards":    table.BottomCards,
				},
				ResultData: map[string]interface{}{
					"multiplier":   bottomMultiplier,
					"bottomPoints": bottomPoints,
				},
			})

			result.FinalScore = totalPoints

			// Determine winner team based on score
			if totalPoints > 120 {
				result.WinnerTeam = "guest" // 抓分方获胜（得分超过120）
			} else {
				result.WinnerTeam = "host" // 庄家方获胜（得分≤120）
			}

			// Calculate level changes
			game, err := GetGame(gameID)
			if err == nil && game != nil {
				// Determine if solo mode (friend not revealed or no friend)
				isSolo := !table.FriendRevealed
				winnerIsHost := result.WinnerTeam == "host"
				levelUp := CalculateLevelUp(totalPoints, isSolo, winnerIsHost)

				gameResults := make([]GameResult, 0)
				for _, playerID := range game.PlayerIDs {
					user, err := GetUserByID(playerID)
					if err != nil {
						continue
					}

					oldLevel := user.Level
					newLevel := oldLevel
					isWinner := false

					// Find player seat
					playerSeat := 0
					for seat, hand := range table.PlayerHands {
						if hand.UserID == playerID {
							playerSeat = seat
							break
						}
					}

					// Determine if winner
					if result.WinnerTeam == "host" {
						isWinner = (playerSeat == table.DealerSeat) || (table.FriendRevealed && playerSeat == table.FriendSeat)
					} else {
						isWinner = (playerSeat != table.DealerSeat) && (!table.FriendRevealed || playerSeat != table.FriendSeat)
					}

					// Update level for winners
					if isWinner && levelUp > 0 {
						newLevel = upgradeLevel(oldLevel, levelUp)
					}

					gameResults = append(gameResults, GameResult{
						UserID:   playerID,
						OldLevel: oldLevel,
						NewLevel: newLevel,
						IsWinner: isWinner,
						Score:    totalPoints,
					})
				}

				result.GameResults = gameResults

				// Record game result and create replay
				friendSeat := table.FriendSeat
				if !table.FriendRevealed {
					friendSeat = 0
				}
				if err := RecordGameResult(gameID, gameResults, table.DealerSeat, result.WinnerTeam, friendSeat, isSolo, true); err != nil {
					fmt.Printf("Failed to record game result: %v\n", err)
				}

				// 存储结算信息到 table，供前端读取
				table.TotalPoints = totalPoints
				table.RoundResults = gameResults
				table.NextRoundCountdownStart = time.Now().UTC().Format(time.RFC3339)
				table.PreviousResult = &PreviousGameResult{
					WinnerTeam: result.WinnerTeam,
					DealerSeat: table.DealerSeat,
					FriendSeat: friendSeat,
					IsSoloMode: isSolo,
				}

				// Create game replay
				initialState := map[string]interface{}{
					"dealerSeat": table.DealerSeat,
					"trumpSuit":  table.TrumpSuit,
					"trumpRank":  table.TrumpRank,
				}

				finalState := map[string]interface{}{
					"totalPoints": totalPoints,
					"winnerTeam":  result.WinnerTeam,
					"results":     gameResults,
				}

				// Get action count
				actions, _ := GetGameActionLogs(gameID)
				totalActions := len(actions)

				// Calculate duration
				durationSeconds := 0
				if len(actions) > 0 {
					startTime := actions[0].Timestamp
					endTime := actions[len(actions)-1].Timestamp
					durationSeconds = int(endTime.Sub(startTime).Seconds())
				}

				if err := CreateGameReplay(gameID, initialState, finalState, totalActions, durationSeconds, result.WinnerTeam, totalPoints); err != nil {
					fmt.Printf("Failed to create game replay: %v\n", err)
				}
			}
		}
	} else {
		// Next player (counter-clockwise: 1→5→4→3→2→1)
		result.NextPlayer = ((playerSeat - 2 + 5) % 5) + 1
		table.CurrentPlayer = result.NextPlayer
	}

	table.LastPlay = result

	// Save the updated table state back to activeGames
	activeGames[gameID] = table

	return result, nil
}

// validateCardPlay validates if the selected cards form a valid play
func validateCardPlay(cards []Card, table *GameTable, hand *PlayerHand) error {
	if len(cards) == 0 {
		return fmt.Errorf("没有选择牌")
	}

	// Check if this is the first play of the trick
	isLead := len(table.CurrentTrick) == 0

	if isLead {
		// Leading: can play single card, pair, triple, or tractor
		return validateLeadPlay(cards, table)
	} else {
		// Following: must follow the lead card type and suit
		return validateFollowPlay(cards, table, hand)
	}
}

// validateLeadPlay validates a lead play
func validateLeadPlay(cards []Card, table *GameTable) error {
	if len(cards) == 1 {
		// Single card is always valid
		return nil
	}

	// Check for tractor (consecutive pairs or triples of same suit)
	if len(cards) >= 4 {
		if err := validateTractor(cards, table); err == nil {
			return nil // Valid tractor
		}
		// If not a valid tractor, continue to check pair/triple
	}

	// For pairs and triples, all cards must have same value AND same suit
	firstValue := cards[0].Value
	firstSuit := cards[0].Suit

	for _, card := range cards {
		if card.Value != firstValue {
			return fmt.Errorf("对子/三张必须是相同点数的牌")
		}
		if card.Suit != firstSuit {
			return fmt.Errorf("对子/三张必须是相同花色的牌")
		}
	}

	// Pair (2 cards) or Triple (3 cards) with same value and suit is valid
	if len(cards) == 2 || len(cards) == 3 {
		return nil
	}

	// For more than 3 cards with mixed values, check if it's a valid throw (甩牌)
	// Throw cards must be same suit
	allSameSuit := true
	for _, card := range cards {
		if card.Suit != firstSuit {
			allSameSuit = false
			break
		}
	}

	if !allSameSuit {
		return fmt.Errorf("无效的牌型：同花色牌才能一起出")
	}

	// This is a potential throw - validate it properly
	// For now, accept same-suit combinations as valid throws
	// The game logic will handle the actual throw validation
	return nil
}

// ThrowCardsResult represents the result of a throw cards validation
type ThrowCardsResult struct {
	IsValid       bool        // Whether the throw is valid
	ActualPlay    []Card      // Cards that should actually be played
	ReturnedCards []Card      // Cards that should be returned to hand
	Reason        string      // Reason for failure or success
	BlockerSeat   int         // 让甩牌失败的玩家座位号（用于高亮显示）
	BlockerCard   string      // 让甩牌失败的牌（用于显示）
	CanChoose     bool        // 是否有多种牌型可选
	ChoiceOptions []CardGroup // 可选的牌型列表
	SelectedType  string      // 被选择管上的牌型
}

// CardGroup represents a group of cards by type
type CardGroup struct {
	Type  string // "triple", "pair", "single"
	Cards []Card
	Value string // Card value (for comparison)
}

// ValidateThrowCards validates if a throw (甩牌) is valid
// 规则：先看牌型，再看点数。按逆时针顺序询问每个玩家是否能管上。
func ValidateThrowCards(cards []Card, table *GameTable, playerSeat int) *ThrowCardsResult {
	if len(cards) < 2 {
		return &ThrowCardsResult{
			IsValid:    true,
			ActualPlay: cards,
			Reason:     "单张不算甩牌",
		}
	}

	// 检查是否同花色
	firstSuit := cards[0].Suit
	for _, card := range cards {
		if card.Suit != firstSuit {
			return &ThrowCardsResult{
				IsValid:    false,
				ActualPlay: cards,
				Reason:     "甩牌必须是同花色",
			}
		}
	}

	// 按牌型分组：三张、对子、单张
	groups := groupCardsByType(cards)

	// 按逆时针顺序询问每个玩家
	// 逆时针顺序：playerSeat -> (playerSeat-1+5)%5+1 -> ...
	for i := 1; i <= 4; i++ {
		seat := ((playerSeat-1-i+5)%5 + 1)
		hand, ok := table.PlayerHands[seat]
		if !ok {
			continue
		}

		// 检查该玩家是否能管上任意一种牌型
		canBeat := make(map[string]bool)        // 记录哪些牌型可以被管上
		blockerCards := make(map[string]string) // 记录用什么牌管上

		for _, group := range groups {
			switch group.Type {
			case "triple":
				// 检查是否有更大的三张
				if largerCard := getLargerTriple(hand.Cards, firstSuit, group.Value); largerCard != "" {
					canBeat["triple"] = true
					blockerCards["triple"] = largerCard
				}
			case "pair":
				// 检查是否有更大的对子
				if largerCard := getLargerPair(hand.Cards, firstSuit, group.Value); largerCard != "" {
					canBeat["pair"] = true
					blockerCards["pair"] = largerCard
				}
			case "single":
				// 检查是否有更大的单张
				if largerCard := getLargerSingle(hand.Cards, firstSuit, group.Value); largerCard != "" {
					canBeat["single"] = true
					blockerCards["single"] = largerCard
				}
			}
		}

		// 如果能管上任意一种牌型，甩牌失败
		if len(canBeat) > 0 {
			// 收集被管上的牌型
			var beatenGroups []CardGroup
			for _, group := range groups {
				if canBeat[group.Type] {
					beatenGroups = append(beatenGroups, group)
				}
			}

			// 判断是否有多种牌型可选
			canChoose := len(canBeat) > 1

			// 找出被管上的牌型中最小的牌
			var keepGroup *CardGroup
			var selectedType string
			var blockerCard string

			if canChoose {
				// 多种牌型被管上，需要玩家选择
				// 默认选择优先级最低的牌型（单张 > 对子 > 三张）
				for _, group := range groups {
					if canBeat[group.Type] {
						if keepGroup == nil || getTypePriority(group.Type) < getTypePriority(keepGroup.Type) {
							keepGroup = &group
							selectedType = group.Type
							blockerCard = blockerCards[group.Type]
						}
					}
				}
			} else {
				// 只有一种牌型被管上，自动处理
				for _, group := range groups {
					if canBeat[group.Type] {
						keepGroup = &group
						selectedType = group.Type
						blockerCard = blockerCards[group.Type]
						break
					}
				}
			}

			// 找出该牌型中最小的牌
			smallestGroup := findSmallestGroupOfType(groups, selectedType, table.TrumpSuit, table.TrumpRank)
			if smallestGroup != nil {
				keepGroup = smallestGroup
			}

			// 计算需要收回的牌
			var returnedCards []Card
			for _, group := range groups {
				if group.Value != keepGroup.Value || group.Type != keepGroup.Type {
					returnedCards = append(returnedCards, group.Cards...)
				}
			}

			// 返回结果
			return &ThrowCardsResult{
				IsValid:       false,
				ActualPlay:    keepGroup.Cards,
				ReturnedCards: returnedCards,
				Reason:        fmt.Sprintf("玩家%d的%s管上了你的%s", seat, blockerCard, describeGroup(*keepGroup)),
				BlockerSeat:   seat,
				BlockerCard:   blockerCard,
				CanChoose:     canChoose,
				ChoiceOptions: beatenGroups,
				SelectedType:  selectedType,
			}
		}
	}

	// 甩牌成功
	return &ThrowCardsResult{
		IsValid:    true,
		ActualPlay: cards,
		Reason:     "甩牌成功",
	}
}

// groupCardsByType groups cards by type (triple, pair, single)
func groupCardsByType(cards []Card) []CardGroup {
	// 统计每个点数的牌
	valueCounts := make(map[string][]Card)
	for _, card := range cards {
		valueCounts[card.Value] = append(valueCounts[card.Value], card)
	}

	var groups []CardGroup

	// 先处理三张
	for value, cards := range valueCounts {
		if len(cards) >= 3 {
			groups = append(groups, CardGroup{
				Type:  "triple",
				Cards: cards[:3],
				Value: value,
			})
			valueCounts[value] = cards[3:] // 移除已处理的牌
		}
	}

	// 再处理对子
	for value, cards := range valueCounts {
		if len(cards) >= 2 {
			groups = append(groups, CardGroup{
				Type:  "pair",
				Cards: cards[:2],
				Value: value,
			})
			valueCounts[value] = cards[2:] // 移除已处理的牌
		}
	}

	// 最后处理单张
	for value, cards := range valueCounts {
		for _, card := range cards {
			groups = append(groups, CardGroup{
				Type:  "single",
				Cards: []Card{card},
				Value: value,
			})
		}
	}

	return groups
}

// hasLargerTriple checks if hand has larger triple of the same suit
func hasLargerTriple(handCards []Card, suit, value string) bool {
	return getLargerTriple(handCards, suit, value) != ""
}

// getLargerTriple returns the larger triple card value, or empty string if none
func getLargerTriple(handCards []Card, suit, value string) string {
	valueCounts := make(map[string]int)
	for _, card := range handCards {
		if card.Suit == suit {
			valueCounts[card.Value]++
		}
	}

	targetValue := getCardNumericValue(value)
	var largerValue string
	var largerNumeric int = -1
	for v, count := range valueCounts {
		numericV := getCardNumericValue(v)
		if count >= 3 && numericV > targetValue {
			if largerValue == "" || numericV < largerNumeric {
				largerValue = v
				largerNumeric = numericV
			}
		}
	}
	if largerValue != "" {
		return fmt.Sprintf("三张%s%s", getSuitDisplayName(suit), largerValue)
	}
	return ""
}

// hasLargerPair checks if hand has larger pair of the same suit
func hasLargerPair(handCards []Card, suit, value string) bool {
	return getLargerPair(handCards, suit, value) != ""
}

// getLargerPair returns the larger pair card value, or empty string if none
func getLargerPair(handCards []Card, suit, value string) string {
	valueCounts := make(map[string]int)
	for _, card := range handCards {
		if card.Suit == suit {
			valueCounts[card.Value]++
		}
	}

	targetValue := getCardNumericValue(value)
	var largerValue string
	var largerNumeric int = -1
	for v, count := range valueCounts {
		numericV := getCardNumericValue(v)
		if count >= 2 && numericV > targetValue {
			if largerValue == "" || numericV < largerNumeric {
				largerValue = v
				largerNumeric = numericV
			}
		}
	}
	if largerValue != "" {
		return fmt.Sprintf("对%s%s", getSuitDisplayName(suit), largerValue)
	}
	return ""
}

// hasLargerSingle checks if hand has larger single of the same suit
func hasLargerSingle(handCards []Card, suit, value string) bool {
	return getLargerSingle(handCards, suit, value) != ""
}

// getLargerSingle returns the larger single card value, or empty string if none
func getLargerSingle(handCards []Card, suit, value string) string {
	targetValue := getCardNumericValue(value)
	var largerValue string
	var largerNumeric int = -1
	for _, card := range handCards {
		numericV := getCardNumericValue(card.Value)
		if card.Suit == suit && numericV > targetValue {
			if largerValue == "" || numericV < largerNumeric {
				largerValue = card.Value
				largerNumeric = numericV
			}
		}
	}
	if largerValue != "" {
		return fmt.Sprintf("%s%s", getSuitDisplayName(suit), largerValue)
	}
	return ""
}

// getTypePriority returns priority of card type (higher is better)
func getTypePriority(cardType string) int {
	switch cardType {
	case "triple":
		return 3
	case "pair":
		return 2
	case "single":
		return 1
	default:
		return 0
	}
}

// describeGroup describes a card group
func describeGroup(group CardGroup) string {
	suit := getSuitDisplayName(group.Cards[0].Suit)
	switch group.Type {
	case "triple":
		return fmt.Sprintf("三张%s%s", suit, group.Value)
	case "pair":
		return fmt.Sprintf("对%s%s", suit, group.Value)
	case "single":
		return fmt.Sprintf("单张%s%s", suit, group.Value)
	default:
		return "未知牌型"
	}
}

// findSmallestGroupOfType finds the smallest group of a specific type
func findSmallestGroupOfType(groups []CardGroup, cardType, trumpSuit, trumpRank string) *CardGroup {
	var smallest *CardGroup
	var smallestValue int = 999

	for i := range groups {
		group := &groups[i]
		if group.Type == cardType {
			value := getCardNumericValue(group.Value)
			if smallest == nil || value < smallestValue {
				smallest = group
				smallestValue = value
			}
		}
	}

	return smallest
}

// findMinCard finds the card with minimum value in a slice
func findMinCard(cards []Card) Card {
	minCard := cards[0]
	minValue := getCardNumericValue(cards[0].Value)

	for _, card := range cards {
		value := getCardNumericValue(card.Value)
		if value < minValue {
			minValue = value
			minCard = card
		}
	}

	return minCard
}

// removeCardFromSlice removes a card from a slice (first occurrence)
func removeCardFromSlice(cards []Card, target Card) []Card {
	result := make([]Card, 0, len(cards))
	found := false
	for _, card := range cards {
		if !found && card.Suit == target.Suit && card.Value == target.Value {
			found = true
			continue
		}
		result = append(result, card)
	}
	return result
}

// getSuitDisplayName returns the Chinese display name of a suit
func getSuitDisplayName(suit string) string {
	names := map[string]string{
		"spades":   "黑桃",
		"hearts":   "红桃",
		"clubs":    "梅花",
		"diamonds": "方片",
		"joker":    "王",
	}
	return names[suit]
}

// validateTractor validates if cards form a tractor (consecutive pairs or triples of same suit)
// 规则：连对（2对以上）或连三（2组以上三张）
// 注意：需要考虑级牌跳过的情况，如打5级时，副牌4-6是连续的（跳过5）
func validateTractor(cards []Card, table *GameTable) error {
	// Tractor must have at least 4 cards (2 pairs) or 6 cards (2 triples)
	if len(cards) < 4 {
		return fmt.Errorf("拖拉机至少需要4张牌(2个对子)或6张牌(2个三张)")
	}

	// All cards must have the same suit
	firstSuit := cards[0].Suit
	for _, card := range cards {
		if card.Suit != firstSuit {
			return fmt.Errorf("拖拉机的所有牌必须是相同花色")
		}
	}

	// Count occurrences of each value
	valueCounts := make(map[string]int)
	for _, card := range cards {
		valueCounts[card.Value]++
	}

	// Check if all values have the same count (either all 2s for pairs, or all 3s for triples)
	var expectedCount int
	firstValue := true
	for _, count := range valueCounts {
		if firstValue {
			expectedCount = count
			firstValue = false
			// Must be either 2 (pairs) or 3 (triples)
			if expectedCount != 2 && expectedCount != 3 {
				return fmt.Errorf("拖拉机必须由连续的对子或连续的三张组成")
			}
		} else {
			if count != expectedCount {
				return fmt.Errorf("拖拉机必须全部是对子或全部是三张，不能混合")
			}
		}
	}

	// Verify total card count matches
	if len(cards) != len(valueCounts)*expectedCount {
		return fmt.Errorf("拖拉机牌数无效")
	}

	// Must have at least 2 groups
	if len(valueCounts) < 2 {
		return fmt.Errorf("拖拉机至少需要2组牌")
	}

	// Check if values are consecutive using the card rank system
	// This correctly handles trump rank skipping
	// Example: if trump rank is 5, then 4-6 in non-trump suit is consecutive

	// Create a list of unique cards (one per value)
	uniqueCards := make([]Card, 0, len(valueCounts))
	for value := range valueCounts {
		// Find a card with this value
		for _, card := range cards {
			if card.Value == value {
				uniqueCards = append(uniqueCards, card)
				break
			}
		}
	}

	// Sort cards by their rank in the game
	sort.Slice(uniqueCards, func(i, j int) bool {
		rankI := getCardRank(uniqueCards[i], table.TrumpSuit, table.TrumpRank)
		rankJ := getCardRank(uniqueCards[j], table.TrumpSuit, table.TrumpRank)
		return rankI < rankJ
	})

	// Check if ranks are consecutive
	// For a valid tractor, each card should have rank = previous rank + 1
	// But we need to handle the case where trump rank is skipped

	// Build expected consecutive sequence
	trumpSuit := table.TrumpSuit
	trumpRank := table.TrumpRank

	for i := 1; i < len(uniqueCards); i++ {
		prevRank := getCardRank(uniqueCards[i-1], trumpSuit, trumpRank)
		currRank := getCardRank(uniqueCards[i], trumpSuit, trumpRank)

		// Check if cards are consecutive
		// For trump cards (rank >= 600), they should be consecutive in the 600+ range
		// For non-trump cards (rank < 600), they should be consecutive in the base value range

		if currRank >= 600 && prevRank >= 600 {
			// Both are trump cards (same suit as trump)
			if currRank != prevRank+1 {
				return fmt.Errorf("拖拉机的点数必须连续")
			}
		} else if currRank < 600 && prevRank < 600 {
			// Both are non-trump cards
			// Check if they are consecutive in base value, considering trump rank skip
			prevBase := getCardNumericValue(uniqueCards[i-1].Value)
			currBase := getCardNumericValue(uniqueCards[i].Value)
			trumpRankBase := getCardNumericValue(trumpRank)

			// If trump rank is between prev and curr, they should differ by 2
			// Otherwise they should differ by 1
			if prevBase < trumpRankBase && currBase > trumpRankBase {
				if currBase != prevBase+2 {
					return fmt.Errorf("拖拉机的点数必须连续(考虑级牌跳过)")
				}
			} else if currBase != prevBase+1 {
				return fmt.Errorf("拖拉机的点数必须连续")
			}
		} else {
			// One is trump, one is not - this shouldn't happen as we checked same suit
			return fmt.Errorf("拖拉机不能混合主牌和副牌")
		}
	}

	return nil
}

// getCardNumericValue returns the numeric value for sorting and comparison
func getCardNumericValue(value string) int {
	values := map[string]int{
		"2": 2, "3": 3, "4": 4, "5": 5, "6": 6, "7": 7, "8": 8, "9": 9, "10": 10,
		"J": 11, "Q": 12, "K": 13, "A": 14,
	}
	return values[value]
}

// validateFollowPlay validates following a lead
//
// 规则：
// 1. 有领出花色的牌：出够领出数量即可（最优组合优先：有对子出对子，有三张出三张）
// 2. 没有领出花色：毙牌（主牌）或垫其他副牌
func validateFollowPlay(cards []Card, table *GameTable, hand *PlayerHand) error {
	if len(table.CurrentTrick) == 0 {
		return fmt.Errorf("没有领出的牌")
	}

	// Get the lead play
	leadPlay := table.CurrentTrick[0].Card
	leadSuit := leadPlay.Suit
	leadSeat := table.CurrentTrick[0].Seat

	// Get all cards the leader played
	leadCards := []Card{}
	for _, pc := range table.CurrentTrick {
		if pc.Seat == leadSeat {
			leadCards = append(leadCards, pc.Card)
		} else {
			break
		}
	}

	leadCardCount := len(leadCards)
	leadCardType := analyzeLeadCardType(leadCards, table.TrumpSuit, table.TrumpRank)

	// 验证出牌数量必须与领出数量一致
	if len(cards) != leadCardCount {
		return fmt.Errorf("必须出%d张牌，实际出了%d张", leadCardCount, len(cards))
	}

	// 分类手牌（同花色牌、主牌/级牌、其他副牌）
	handSuitCards := []Card{}
	handTrumpCards := []Card{}
	handOtherCards := []Card{}

	for _, card := range hand.Cards {
		if card.Value == table.TrumpRank || card.Value == "Joker" {
			handTrumpCards = append(handTrumpCards, card)
		} else if card.Suit == leadSuit {
			handSuitCards = append(handSuitCards, card)
		} else {
			handOtherCards = append(handOtherCards, card)
		}
	}

	// 分类本次出的牌
	playedSuitCards := []Card{}
	playedTrumpCards := []Card{}
	playedOtherCards := []Card{}

	for _, card := range cards {
		if card.Value == table.TrumpRank || card.Value == "Joker" {
			playedTrumpCards = append(playedTrumpCards, card)
		} else if card.Suit == leadSuit {
			playedSuitCards = append(playedSuitCards, card)
		} else {
			playedOtherCards = append(playedOtherCards, card)
		}
	}

	// 情况1：玩家有领出花色的牌
	if len(handSuitCards) > 0 {
		// 验证最优组合：交给 validateFollowSuit 检查
		return validateFollowSuit(cards, leadCards, leadCardType, hand.Cards, table.TrumpSuit, table.TrumpRank)
	}

	// 情况2：玩家没有领出花色的牌，可以毙牌或垫其他副牌
	return nil
}

// analyzeLeadCardType 分析领出牌型
// trumpSuit/trumpRank 传递用于正确识别拖拉机（考虑级牌跳过）
func analyzeLeadCardType(leadCards []Card, trumpSuit, trumpRank string) string {
	if len(leadCards) == 1 {
		return "single"
	}

	// 检查是否是对子或三张
	firstValue := leadCards[0].Value
	firstSuit := leadCards[0].Suit
	allSameValue := true
	allSameSuit := true

	for _, card := range leadCards {
		if card.Value != firstValue {
			allSameValue = false
		}
		if card.Suit != firstSuit {
			allSameSuit = false
		}
	}

	if allSameValue && allSameSuit {
		if len(leadCards) == 2 {
			return "pair"
		} else if len(leadCards) == 3 {
			return "triple"
		}
	}

	// 检查是否是拖拉机（传递 trump 上下文以正确处理级牌跳过）
	if len(leadCards) >= 4 && isTractorWithContext(leadCards, trumpSuit, trumpRank) {
		return "tractor"
	}

	// 检查是否是混合甩牌
	if allSameSuit && len(leadCards) > 1 {
		return "throw"
	}

	return "mixed"
}

// validateFollowSuit 验证有领出花色时的跟牌是否合法
//
// 规则（按优先级）：
// 1. 领出三张：有三张必须跟三张；没有三张有对子必须跟对子；没有三张和对子才出散牌
// 2. 领出对子：有对子必须跟对子；没有对子出散牌
// 3. 领出拖拉机/甩牌（>=4张）：凑够数量即可
// 4. 有色跟色，色绝了后可以随便出（任意牌，不一定是主牌）
func validateFollowSuit(cards []Card, leadCards []Card, leadCardType string, handCards []Card, trumpSuit string, trumpRank string) error {
	leadCardCount := len(leadCards)
	leadCard := leadCards[0]

	// 验证出牌数量必须与领出数量一致
	if len(cards) != leadCardCount {
		return fmt.Errorf("必须出%d张牌", leadCardCount)

	}

	// 分类手牌（同花色牌、其他牌）
	// 规则：领出主牌时，所有主牌视为同花色；领出副牌时，同花色副牌（排除主牌）视为同花色
	handSuitCards := []Card{}
	handOtherCards := []Card{}
	for _, card := range handCards {
		if isSameSuitForFollow(card, leadCard, trumpSuit, trumpRank) {
			handSuitCards = append(handSuitCards, card)
		} else {
			handOtherCards = append(handOtherCards, card)
		}
	}

	// 分类本次出的牌
	playedSuitCards := []Card{}
	playedOtherCards := []Card{}
	for _, card := range cards {
		if isSameSuitForFollow(card, leadCard, trumpSuit, trumpRank) {
			playedSuitCards = append(playedSuitCards, card)
		} else {
			playedOtherCards = append(playedOtherCards, card)
		}
	}

	// 分析手牌中的牌型（只看同花色牌）
	// 对子/三张必须同花色且同点数（RULE.md 5.2），所以按「花色+点数」统计，
	// 不能只按点数——领出主牌时同花色牌会横跨多个花色（王、各花色级牌、主花色牌）。
	valueCounts := suitValueCounts(handSuitCards)

	hasTriple := false
	hasPair := false
	hasTractor := false
	for _, count := range valueCounts {
		if count >= 3 {
			hasTriple = true
		}
		if count >= 2 {
			hasPair = true
		}
	}
	if len(handSuitCards) >= 4 {
		hasTractor = containsTractor(handSuitCards, trumpRank)
	}

	// 分析玩家出的牌型（传递 trump 上下文以正确识别拖拉机）
	playCardType := analyzeLeadCardType(cards, trumpSuit, trumpRank)

	// 根据领出牌型验证
	switch leadCardType {
	case "triple":
		// 领出三张：有同花色三张必须跟三张
		if hasTriple && playCardType != "triple" {
			return fmt.Errorf("领出三张，必须跟三张")
		}
		// 没有三张有对子必须跟对子（凑够3张：对子+1张补牌）
		if !hasTriple && hasPair {
			// 检查出的牌中是否包含同花色对子（不要求整体牌型为pair，允许pair+散牌凑够3张）
			hasPlayedPair := false
			for _, count := range suitValueCounts(playedSuitCards) {
				if count >= 2 {
					hasPlayedPair = true
					break
				}
			}
			if !hasPlayedPair {
				return fmt.Errorf("领出三张，没有三张必须跟对子")
			}
		}
		// 没有三张也没有对子：出散牌（已由数量验证覆盖）

	case "pair":
		// 领出对子：有同花色对子必须跟对子
		if hasPair && playCardType != "pair" {
			return fmt.Errorf("领出对子，必须跟对子")
		}

	case "tractor", "throw":
		// 领出拖拉机/甩牌：有拖拉机必须跟拖拉机，凑够数量即可
		if hasTractor && playCardType != "tractor" && playCardType != "throw" {
			return fmt.Errorf("领出拖拉机，必须跟拖拉机")
		}

	default:
		// 单张：只要同花色即可
	}

	// 色绝了规则：有色必须全部跟出，色绝了后才可以用任意牌凑够数量
	// 条件：出了其他牌 AND 手里的同花色牌没有全部出完
	if len(playedOtherCards) > 0 && len(playedSuitCards) < len(handSuitCards) {
		return fmt.Errorf("有%d张同花色必须全部跟出，不能出其他花色", len(handSuitCards))
	}

	// 色绝了后：只要凑够数量，可以用任意牌（包括任意副牌，不一定是主牌）
	// 色绝了的条件：手里的同花色牌全部出完（playedSuitCards == handSuitCards < leadCardCount）

	return nil
}

// suitValueCounts 按「花色+点数」统计张数。
// 对子/三张必须同花色且同点数（RULE.md 5.2），所以不能只按点数统计。
func suitValueCounts(cards []Card) map[string]int {
	counts := make(map[string]int)
	for _, card := range cards {
		counts[card.Suit+"|"+card.Value]++
	}
	return counts
}

// containsTractor 检查手牌中是否包含拖拉机
// 拖拉机是同花色内连续的对子或三张，所以按花色分别检查
func containsTractor(handSuitCards []Card, trumpRank string) bool {
	bySuit := make(map[string][]Card)
	for _, card := range handSuitCards {
		bySuit[card.Suit] = append(bySuit[card.Suit], card)
	}

	for _, cards := range bySuit {
		valueCounts := make(map[string]int)
		for _, card := range cards {
			valueCounts[card.Value]++
		}

		pairs := []string{}   // 有对子的点数
		triples := []string{} // 有三张的点数
		for value, count := range valueCounts {
			if count >= 3 {
				triples = append(triples, value)
			} else if count >= 2 {
				pairs = append(pairs, value)
			}
		}

		// 检查三张是否能构成拖拉机（至少2组连续的三张）
		if len(triples) >= 2 && hasConsecutiveValues(triples, trumpRank) {
			return true
		}

		// 检查对子是否能构成拖拉机（至少2组连续的对子）
		if len(pairs) >= 2 && hasConsecutiveValues(pairs, trumpRank) {
			return true
		}
	}

	return false
}

// hasConsecutiveValues 检查给定的点数数组是否全部连续
// 考虑级牌跳过的情况：副牌等级连续=差1，或差2且中间是级牌
// 例如 trumpRank=5 时，4-6-7 连续（4跳5到6，6到7差1）
// 例如 trumpRank=5 时，4-6-8 不连续（4跳5到6，但6到8中间是7不是5）
func hasConsecutiveValues(values []string, trumpRank string) bool {
	if len(values) < 2 {
		return false
	}

	// 按点数排序
	sort.Slice(values, func(i, j int) bool {
		return getCardNumericValue(values[i]) < getCardNumericValue(values[j])
	})

	trumpRankBase := getCardNumericValue(trumpRank)

	// 检查每一对是否都连续（不是"任意一对"）
	for i := 1; i < len(values); i++ {
		prevBase := getCardNumericValue(values[i-1])
		currBase := getCardNumericValue(values[i])

		// 正常连续（差1）
		if currBase == prevBase+1 {
			continue
		}

		// 跨级牌连续（差2，且级牌在中间）
		if currBase == prevBase+2 && prevBase < trumpRankBase && currBase > trumpRankBase {
			continue
		}

		// 任何一对不连续 → 整体不连续
		return false
	}

	return true
}

// isTractor checks if the cards form a tractor (consecutive pairs or triples)
// 注意：此函数仅作为快速检查，不考虑级牌跳过的情况
// 完整验证请使用 validateTractor 或 isTractorWithContext
func isTractor(cards []Card) bool {
	if len(cards) < 4 {
		return false
	}
	firstSuit := cards[0].Suit
	for _, card := range cards {
		if card.Suit != firstSuit {
			return false
		}
	}

	valueCounts := make(map[string]int)
	for _, card := range cards {
		valueCounts[card.Value]++
	}

	// Check if all values have the same count (either all 2s or all 3s)
	var expectedCount int
	firstValue := true
	for _, count := range valueCounts {
		if firstValue {
			expectedCount = count
			firstValue = false
			// Must be either 2 (pairs) or 3 (triples)
			if expectedCount != 2 && expectedCount != 3 {
				return false
			}
		} else {
			if count != expectedCount {
				return false
			}
		}
	}

	// Must have at least 2 groups
	if len(valueCounts) < 2 {
		return false
	}

	values := make([]string, 0, len(valueCounts))
	for value := range valueCounts {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		return getCardNumericValue(values[i]) < getCardNumericValue(values[j])
	})

	for i := 1; i < len(values); i++ {
		prevValue := getCardNumericValue(values[i-1])
		currValue := getCardNumericValue(values[i])
		if currValue != prevValue+1 {
			return false
		}
	}
	return true
}

// isTractorWithContext checks if the cards form a tractor considering trump rank skip
// 考虑级牌跳过的情况，例如打5级时，副4-6是连续的
func isTractorWithContext(cards []Card, trumpSuit, trumpRank string) bool {
	if len(cards) < 4 {
		return false
	}
	firstSuit := cards[0].Suit
	for _, card := range cards {
		if card.Suit != firstSuit {
			return false
		}
	}

	valueCounts := make(map[string]int)
	for _, card := range cards {
		valueCounts[card.Value]++
	}

	// Check if all values have the same count (either all 2s or all 3s)
	var expectedCount int
	firstValue := true
	for _, count := range valueCounts {
		if firstValue {
			expectedCount = count
			firstValue = false
			// Must be either 2 (pairs) or 3 (triples)
			if expectedCount != 2 && expectedCount != 3 {
				return false
			}
		} else {
			if count != expectedCount {
				return false
			}
		}
	}

	// Must have at least 2 groups
	if len(valueCounts) < 2 {
		return false
	}

	// Create a list of unique cards (one per value)
	uniqueCards := make([]Card, 0, len(valueCounts))
	for value := range valueCounts {
		for _, card := range cards {
			if card.Value == value {
				uniqueCards = append(uniqueCards, card)
				break
			}
		}
	}

	// Sort by numeric value for checking consecutiveness
	sort.Slice(uniqueCards, func(i, j int) bool {
		return getCardNumericValue(uniqueCards[i].Value) < getCardNumericValue(uniqueCards[j].Value)
	})

	// Check if values are consecutive, considering trump rank skip
	trumpRankBase := getCardNumericValue(trumpRank)
	isTrump := firstSuit == trumpSuit

	for i := 1; i < len(uniqueCards); i++ {
		prevBase := getCardNumericValue(uniqueCards[i-1].Value)
		currBase := getCardNumericValue(uniqueCards[i].Value)

		// For trump suit, values should be strictly consecutive
		if isTrump {
			if currBase != prevBase+1 {
				return false
			}
		} else {
			// For non-trump suits, check if trump rank is skipped
			if prevBase < trumpRankBase && currBase > trumpRankBase {
				// Trump rank is between prev and curr, they should differ by 2
				if currBase != prevBase+2 {
					return false
				}
			} else if currBase != prevBase+1 {
				return false
			}
		}
	}
	return true
}

// hasPairInSuit checks if player has a pair in the given suit
func hasPairInSuit(cards []Card, suit string, trumpRank string) bool {
	suitCards := make(map[string]int)
	for _, card := range cards {
		// 级牌不算在原花色中（级牌是主牌的一部分）
		if card.Value == trumpRank {
			continue
		}
		if card.Suit == suit {
			suitCards[card.Value]++
		}
	}
	for _, count := range suitCards {
		if count >= 2 {
			return true
		}
	}
	return false
}

// validateSingleSuitFollow validates following with single cards (when can't match pair/triple)
func validateSingleSuitFollow(cards []Card, leadSuit string, trumpRank string) error {
	// Check if player has any card of lead suit
	// 注意：级牌属于主牌，不属于其原花色
	hasLeadSuit := false
	for _, card := range cards {
		// 级牌不算在原花色中（级牌是主牌的一部分）
		if card.Value == trumpRank {
			continue
		}
		if card.Suit == leadSuit {
			hasLeadSuit = true
			break
		}
	}

	if hasLeadSuit {
		// If player has lead suit, all played cards must be of lead suit (excluding trump rank cards)
		for _, card := range cards {
			// 级牌可以不跟色（因为它是主牌）
			if card.Value == trumpRank {
				continue
			}
			if card.Suit != leadSuit {
				return fmt.Errorf("有该花色必须跟该花色")
			}
		}
	}

	return nil
}

// ==================== 亮庄/反庄相关函数 ====================

// CallDealer 玩家亮庄/反庄
// 用级牌亮庄决定主牌花色；详见 rules/03-bidding.md §3.2 §3.3
// 允许在发牌阶段(dealing)和倒计时阶段(calling)亮庄
func CallDealer(gameID, userID string, suit string, cardIndices []int) (*GameTable, error) {
	lock := getGameLock(gameID)
	lock.Lock()
	defer lock.Unlock()

	table, err := GetTableGame(gameID)
	if err != nil {
		return nil, err
	}

	// 允许在发牌阶段或倒计时阶段亮庄
	if table.Status != "calling" && table.Status != "dealing" {
		return nil, fmt.Errorf("游戏不在亮庄或发牌阶段")
	}

	// Find player's seat
	var playerSeat int
	var hand *PlayerHand
	for seat, h := range table.PlayerHands {
		if h.UserID == userID {
			playerSeat = seat
			hand = h
			break
		}
	}

	if hand == nil {
		return nil, fmt.Errorf("玩家不在游戏中")
	}

	// 允许在发牌阶段或倒计时阶段亮庄
	if table.CallPhase != "counting" && table.CallPhase != "dealing" {
		return nil, fmt.Errorf("不在亮庄倒计时或发牌阶段")
	}

	// 注意：亮庄/反庄的判定只看"张数比上一次多 + 不超过 3 张"，与"谁亮过"无关。
	// 即使同一玩家在被反走之后再反回来也是合法的，由下方反庄规则统一把关。

	// 检查玩家是否已经选择不叫庄
	for _, seat := range table.PassedSeats {
		if seat == playerSeat {
			return nil, fmt.Errorf("你已经选择不叫庄")
		}
	}

	// 使用游戏的当前等级，而不是玩家的个人等级
	// 在升级游戏中，所有玩家使用相同的等级（从2开始）
	gameLevel := table.CurrentLevel
	// 备用：如果 CurrentLevel 为空，使用 TrumpRank 或默认值 "2"
	if gameLevel == "" {
		gameLevel = table.TrumpRank
	}
	if gameLevel == "" {
		gameLevel = "2" // 默认从2级开始
		log.Printf("[CallDealer] WARNING: CurrentLevel and TrumpRank are empty, using default '2'")
	}

	// 调试：打印完整的表格信息
	log.Printf("[CallDealer] DEBUG: table.CurrentLevel='%s', TrumpRank='%s', final gameLevel='%s'",
		table.CurrentLevel, table.TrumpRank, gameLevel)

	// 确定应该验证的级牌点数
	var rank string
	isFirstCall := len(table.CallRecords) == 0

	log.Printf("[CallDealer] userID=%s, gameLevel='%s', isFirstCall=%v, CallRecords count=%d",
		userID, gameLevel, isFirstCall, len(table.CallRecords))

	if isFirstCall {
		// 首次叫庄：必须使用游戏的当前级牌
		rank = gameLevel
	} else {
		// 反庄：可以使用临时庄家的级牌或游戏的当前级牌
		// lastCall := table.CallRecords[len(table.CallRecords)-1]
		// 这里先不限定，让玩家出牌后再验证是哪种情况
		rank = "" // 暂时不验证，允许两种级牌
	}

	// Validate card indices and check they are rank cards
	var cardsToPlay []Card
	for _, idx := range cardIndices {
		if idx < 0 || idx >= len(hand.Cards) {
			return nil, fmt.Errorf("无效的牌索引")
		}
		card := hand.Cards[idx]

		// 检查是否是级牌
		if isFirstCall {
			// 首次叫庄：必须是当前等级的级牌
			log.Printf("[CallDealer] Checking card: value=%s, rank=%s, match=%v", card.Value, rank, card.Value == rank)
			if card.Value != rank {
				return nil, fmt.Errorf("首次叫庄需要使用 %s 级牌", rank)
			}
		} else {
			// 反庄：可以是临时庄家的级牌或游戏的当前级牌
			lastCall := table.CallRecords[len(table.CallRecords)-1]
			if card.Value != lastCall.Rank && card.Value != gameLevel {
				return nil, fmt.Errorf("反庄必须使用临时庄家的级牌或当前级牌")
			}
			// 记录实际使用的级牌
			if rank == "" {
				rank = card.Value
			} else if rank != card.Value {
				return nil, fmt.Errorf("叫庄的级牌必须是同一点数")
			}
		}
		cardsToPlay = append(cardsToPlay, card)
	}

	// 如果没有提供花色，自动从选中的牌中提取
	if suit == "" {
		if len(cardsToPlay) == 0 {
			return nil, fmt.Errorf("至少需要选择一张级牌")
		}
		suit = cardsToPlay[0].Suit
		log.Printf("[CallDealer] Auto-extracted suit from cards: %s", suit)
	}

	// 检查是否是同花色的级牌
	firstSuit := cardsToPlay[0].Suit
	for _, card := range cardsToPlay {
		if card.Suit != firstSuit {
			return nil, fmt.Errorf("叫庄的级牌必须是同花色")
		}
	}

	// 记录叫庄
	table.CallRecords = append(table.CallRecords, CallRecord{
		Seat:      playerSeat,
		Suit:      suit,
		Rank:      rank,
		Count:     len(cardsToPlay),
		Timestamp: time.Now().UnixNano(),
	})

	// 检查反庄（如果之前已经有人叫庄）
	if len(table.CallRecords) > 1 {
		lastCall := table.CallRecords[len(table.CallRecords)-2]

		// 反庄基础规则：必须比临时庄家亮的牌多至少一张
		if len(cardsToPlay) <= lastCall.Count {
			return nil, fmt.Errorf("反庄张数必须多于当前叫庄")
		}
		if len(cardsToPlay) > 3 {
			return nil, fmt.Errorf("反主最多3张")
		}

		// 判断反庄方式
		// 特殊情况：当反庄者的级牌也是2（与临时庄家相同）时，用2反庄会转移庄家
		// 方式一：用临时庄家的级牌反庄（rank == lastCall.Rank 且 rank != gameLevel）
		// 方式二：用游戏当前级牌反庄（rank == gameLevel）

		if rank == lastCall.Rank && rank == gameLevel {
			// 特殊情况：反庄者的级牌也是2（与临时庄家相同）
			// 使用2反庄时，庄家转移给反庄者，同时改变主牌花色
			table.TrumpRank = rank
			table.DealerSeat = playerSeat
			table.TrumpSuit = suit
			table.HostID = userID
		} else if rank == lastCall.Rank {
			// 方式一：用临时庄家的级牌反庄（但不是当前级牌）
			// 庄家不变，只变主牌花色
			table.TrumpSuit = suit
			// 庄家保持为lastCall.Seat
			table.DealerSeat = lastCall.Seat
			table.HostID = table.PlayerHands[lastCall.Seat].UserID
		} else if rank == gameLevel {
			// 方式二：用游戏当前级牌反庄
			// 玩家变为临时庄家，主牌花色变为玩家亮的花色
			table.TrumpRank = rank
			table.DealerSeat = playerSeat
			table.TrumpSuit = suit
			table.HostID = userID
		} else {
			return nil, fmt.Errorf("反庄必须使用临时庄家的级牌或自己的级牌")
		}
	}

	// 设置庄家和主牌花色（首次叫庄）
	if len(table.CallRecords) == 1 {
		table.DealerSeat = playerSeat
		table.TrumpSuit = suit
		table.HostID = userID
		table.TrumpRank = rank
	}

	// 亮庄/反庄成功后的倒计时处理（详见 rules/03-bidding.md §3.0 §3.1）：
	// - 发牌期间(DealingPhase=="dealing")：不启动倒计时，CallCountdown 保持 0，
	//   等发完最后一张牌后由 DealNextCard 统一启动 10 秒初始倒计时。
	// - 发牌完成后(DealingPhase=="finished")：在初始/追加倒计时内发生亮庄/反庄，
	//   倒计时刷新为 5 秒，给其他玩家反庄机会。
	// - 例外：本次亮庄/反庄已经亮到 3 张（封顶），按规则不可能再有人反庄，
	//   直接跳过倒计时进入定庄完成阶段（亮庄定庄不展示底牌）。
	isMaxCallCount := len(cardsToPlay) == 3
	table.CallPhase = "counting" // 标记已有人亮庄，等待可能的反庄
	if table.DealingPhase == "finished" {
		table.CallCountdown = 5
	} else {
		table.CallCountdown = 0
	}

	// 记录亮庄日志
	LogGameAction(GameActionLogRequest{
		GameID:     gameID,
		ActionType: "call_dealer",
		PlayerSeat: playerSeat,
		PlayerID:   userID,
		ActionData: map[string]interface{}{
			"suit":         suit,
			"rank":         rank,
			"count":        len(cardsToPlay),
			"card_indices": cardIndices,
			"cards":        cardsToPlay,
		},
		ResultData: map[string]interface{}{
			"dealer_seat":    table.DealerSeat,
			"trump_suit":     table.TrumpSuit,
			"trump_rank":     table.TrumpRank,
			"call_phase":     table.CallPhase,
			"call_countdown": 5,
		},
	})

	// 单人模式：让AI尝试反庄
	if isSinglePlayerGame(table) {
		// 三张封顶：无人能再反，跳过AI反庄判定与倒计时
		// 注意：定庄 ≠ 停止发牌。
		// - 发牌已完成：立即 finalize 切到扣底牌
		// - 发牌仍在进行：保持发牌节奏继续发完，由 DealNextCard 完成分支检测
		//   "末次亮庄已封顶"并立即 finalize（无需走 10s/5s 倒计时）
		if isMaxCallCount {
			if table.DealingPhase == "finished" {
				advanceCallingPlayer(table)
				table.UpdatedAt = time.Now()
				activeGames[gameID] = table
				return finalizeDealerAndStartPlaying(table)
			}
			// 发牌中：保持 CallPhase=counting + CallCountdown=0，让发牌继续
			advanceCallingPlayer(table)
			table.UpdatedAt = time.Now()
			activeGames[gameID] = table
			return table, nil
		}
		// 检查 AI 是否可以反庄（仅决定是否追加 CallRecords / 更新庄家信息，
		// 不再决定是否提前 finalize）。
		// 倒计时是公平的：不论 AI 是否反庄，只要发牌已完成，就必须等 5 秒追加倒计时
		// 自然结束（让真人玩家有反庄机会），到时由 GetTableGame tick 统一 finalize。
		// 发牌期间则保持 CallCountdown=0，等发完最后一张牌后再启动倒计时。
		_ = tryAICounterCall(table, gameID)
		// 函数顶部已根据 DealingPhase 设好 CallCountdown（finished→5、dealing→0）；
		// 此处不再做任何 finalize / 缩短倒计时的特殊处理。
	} else if isMaxCallCount {
		// 多人模式下三张封顶：跳过反庄等待
		// - 发牌已完成：立即 finalize 切到扣底牌
		// - 发牌仍在进行：发牌继续，由 DealNextCard 完成分支检测封顶并 finalize
		if table.DealingPhase == "finished" {
			advanceCallingPlayer(table)
			table.UpdatedAt = time.Now()
			activeGames[gameID] = table
			return finalizeDealerAndStartPlaying(table)
		}
	}

	// 推进到下一个未行动的叫庄玩家
	advanceCallingPlayer(table)
	table.UpdatedAt = time.Now()

	// 保存游戏状态到内存
	activeGames[gameID] = table
	log.Printf("[CallDealer] Saved game state: CallRecords=%d, CallCountdown=%d, DealerSeat=%d",
		len(table.CallRecords), table.CallCountdown, table.DealerSeat)

	return table, nil
}

// PassCall 玩家选择"不叫庄"
// 详见 rules/03-bidding.md §3.0 §3.1
func PassCall(gameID, userID string) (*GameTable, error) {
	lock := getGameLock(gameID)
	lock.Lock()
	defer lock.Unlock()

	table, err := GetTableGame(gameID)
	if err != nil {
		return nil, err
	}

	// 允许在发牌阶段或倒计时阶段不叫庄
	if table.Status != "calling" && table.Status != "dealing" {
		return nil, fmt.Errorf("游戏不在亮庄或发牌阶段")
	}

	// Find player's seat
	var playerSeat int
	var hand *PlayerHand
	for seat, h := range table.PlayerHands {
		if h.UserID == userID {
			playerSeat = seat
			hand = h
			break
		}
	}

	if hand == nil {
		return nil, fmt.Errorf("玩家不在游戏中")
	}

	if table.CallPhase != "counting" && table.CallPhase != "dealing" {
		return nil, fmt.Errorf("不在叫庄倒计时或发牌阶段")
	}

	// 检查玩家是否已经亮过庄
	for _, record := range table.CallRecords {
		if record.Seat == playerSeat {
			return nil, fmt.Errorf("你已经亮过庄了")
		}
	}

	// 检查玩家是否已经选择不叫庄
	for _, seat := range table.PassedSeats {
		if seat == playerSeat {
			return nil, fmt.Errorf("you have already passed")
		}
	}

	// 记录"不叫庄"
	table.PassedSeats = append(table.PassedSeats, playerSeat)

	// 记录不叫庄日志
	LogGameAction(GameActionLogRequest{
		GameID:     gameID,
		ActionType: "pass_call",
		PlayerSeat: playerSeat,
		PlayerID:   userID,
		ActionData: map[string]interface{}{
			"action": "pass",
		},
		ResultData: map[string]interface{}{
			"passed_seats": table.PassedSeats,
		},
	})

	totalPlayers := len(table.PlayerHands)

	// 情况B：已有人亮庄，检查是否所有未亮庄的玩家都选择了不叫庄
	if len(table.CallRecords) > 0 {
		// 已亮庄的玩家数 + 不叫庄的玩家数 == 总玩家数
		// 说明所有人都做出了选择（亮庄或不叫庄）
		if len(table.CallRecords)+len(table.PassedSeats) == totalPlayers {
			// 确定最后叫庄的人为庄家
			lastCall := table.CallRecords[len(table.CallRecords)-1]
			table.DealerSeat = lastCall.Seat
			table.HostID = table.PlayerHands[lastCall.Seat].UserID
			table.CallPhase = "finished"

			// 记录确定庄家的日志
			LogGameAction(GameActionLogRequest{
				GameID:     gameID,
				ActionType: "dealer_confirmed",
				PlayerSeat: 0,
				PlayerID:   "",
				ActionData: map[string]interface{}{
					"reason": "all_others_passed",
				},
				ResultData: map[string]interface{}{
					"dealer_seat": table.DealerSeat,
					"trump_suit":  table.TrumpSuit,
					"trump_rank":  table.TrumpRank,
				},
			})

			// 如果是单人模式，直接进入找朋友阶段
			if isSinglePlayerGame(table) {
				return finalizeDealerAndStartPlaying(table)
			}

			table.UpdatedAt = time.Now()
			return table, nil
		}
	}

	// 情况A：无人亮庄，检查是否所有人都不叫庄
	if len(table.CallRecords) == 0 && len(table.PassedSeats) == totalPlayers {
		// 规则（rules/03-bidding.md §3.0 §3.1）：
		// - 翻底牌只能发生在"发完牌 + 初始倒计时归零 + 全程无人亮庄"三者都满足后。
		// - 发牌期间(DealingPhase=="dealing")即便所有人都按了不叫庄，也要等发完最后一张牌、
		//   倒计时启动并归零，才能进入 flipping。
		// - 发牌完成后(DealingPhase=="finished")所有人按不叫庄 → 立刻结束倒计时，进入 flipping。
		if table.DealingPhase == "finished" {
			table.CallPhase = "flipping"
			table.CallCountdown = 0
			table.FlipStartedAt = time.Now()

			LogGameAction(GameActionLogRequest{
				GameID:     gameID,
				ActionType: "enter_flipping_phase",
				PlayerSeat: 0,
				PlayerID:   "",
				ActionData: map[string]interface{}{
					"reason": "all_players_passed",
				},
				ResultData: map[string]interface{}{
					"call_phase": "flipping",
				},
			})
		}
		// 发牌期间所有人 pass：保持 dealing 阶段不变，等发完牌后由 DealNextCard
		// 把 CallPhase 切到 counting 并启动 10 秒倒计时，倒计时归零时再由
		// CheckAndProcessCountdown 走到 flipping。
	} else {
		// 推进到下一个未行动的叫庄玩家
		advanceCallingPlayer(table)
	}

	table.UpdatedAt = time.Now()

	// 保存游戏状态到内存
	activeGames[gameID] = table
	log.Printf("[PassCall] Saved game state: PassedSeats=%v, CallRecords=%d",
		table.PassedSeats, len(table.CallRecords))

	return table, nil
}

// CheckAndProcessCountdown 检查并处理倒计时
// 当倒计时为0时自动确定庄家或进入翻底牌阶段
func CheckAndProcessCountdown(gameID string) (*GameTable, error) {
	table, err := GetTableGame(gameID)
	if err != nil {
		return nil, err
	}

	// 规则（rules/03-bidding.md §3.0）：倒计时只在 Status=="calling" 且 CallPhase=="counting" 时生效
	if table.Status != "calling" || table.CallPhase != "counting" {
		return table, nil
	}

	// 倒计时还没结束
	if table.CallCountdown > 0 {
		return table, nil
	}

	// 兜底：发牌尚未完成时不处理（理论上 calling 阶段意味着已发完，但加保护）
	if table.DealingPhase == "dealing" {
		return table, nil
	}

	// 倒计时为0，处理逻辑

	// 情况1：有人叫庄，倒计时结束后确定庄家
	if len(table.CallRecords) > 0 {
		lastCall := table.CallRecords[len(table.CallRecords)-1]
		table.DealerSeat = lastCall.Seat
		table.HostID = table.PlayerHands[lastCall.Seat].UserID
		table.CallPhase = "finished"

		// 记录确定庄家的日志
		LogGameAction(GameActionLogRequest{
			GameID:     gameID,
			ActionType: "dealer_confirmed",
			PlayerSeat: 0,
			PlayerID:   "",
			ActionData: map[string]interface{}{
				"reason": "countdown_ended",
			},
			ResultData: map[string]interface{}{
				"dealer_seat": table.DealerSeat,
				"trump_suit":  table.TrumpSuit,
				"trump_rank":  table.TrumpRank,
			},
		})

		// 确定庄家后，进入叫朋友和扣底牌阶段
		table, _ = finalizeDealerAndStartPlaying(table)
		activeGames[gameID] = table
		return table, nil
	}

	// 情况2：无人亮庄，进入翻底牌阶段
	table.CallPhase = "flipping"
	table.FlipStartedAt = time.Now()

	// 记录进入翻底牌阶段的日志
	LogGameAction(GameActionLogRequest{
		GameID:     gameID,
		ActionType: "enter_flipping_phase",
		PlayerSeat: 0,
		PlayerID:   "",
		ActionData: map[string]interface{}{
			"reason": "countdown_ended_no_caller",
		},
		ResultData: map[string]interface{}{
			"call_phase": "flipping",
		},
	})

	table.UpdatedAt = time.Now()
	activeGames[gameID] = table
	return table, nil
}

// tryAICounterCall 尝试让AI反庄
// 返回true表示有AI反庄，false表示没有AI反庄
func tryAICounterCall(table *GameTable, gameID string) bool {
	if len(table.CallRecords) == 0 {
		return false
	}

	lastCall := table.CallRecords[len(table.CallRecords)-1]
	rank := table.TrumpRank

	// 遍历所有AI玩家（座位2-5）
	for seat := 2; seat <= 5; seat++ {
		// 跳过已经叫过庄或已经pass的玩家
		alreadyCalled := false
		for _, record := range table.CallRecords {
			if record.Seat == seat {
				alreadyCalled = true
				break
			}
		}
		if alreadyCalled {
			continue
		}

		alreadyPassed := false
		for _, passedSeat := range table.PassedSeats {
			if passedSeat == seat {
				alreadyPassed = true
				break
			}
		}
		if alreadyPassed {
			continue
		}

		hand := table.PlayerHands[seat]
		if hand == nil {
			continue
		}

		// 统计AI手中的级牌
		suitCounts := make(map[string][]int) // suit -> card indices
		for idx, card := range hand.Cards {
			if card.Value == rank {
				suitCounts[card.Suit] = append(suitCounts[card.Suit], idx)
			}
		}

		// 找到数量最多的花色
		var bestSuit string
		var bestIndices []int
		for suit, indices := range suitCounts {
			if len(indices) > len(bestIndices) {
				bestSuit = suit
				bestIndices = indices
			}
		}

		// 如果AI的级牌数量比当前庄家多，就反庄
		if len(bestIndices) > lastCall.Count && len(bestIndices) <= 3 {
			// AI反庄
			CallDealer(gameID, hand.UserID, bestSuit, bestIndices)
			return true
		}
	}

	return false
}

// IsAIUserID 判断用户 ID 是否属于机器人。
// AI ID 历史上出现过 "ai_" 和 "ai-" 两种写法，两种都认。
func IsAIUserID(userID string) bool {
	return len(userID) >= 3 && (userID[:3] == "ai_" || userID[:3] == "ai-")
}

// AISeatLabel 取出 AI 用户 ID 里的座位编号，用于拼显示名。
func AISeatLabel(userID string) string {
	if !IsAIUserID(userID) {
		return ""
	}
	return userID[3:]
}

// isSinglePlayerGame checks if this is a single player game
func isSinglePlayerGame(table *GameTable) bool {
	aiCount := 0
	for _, hand := range table.PlayerHands {
		if IsAIUserID(hand.UserID) {
			aiCount++
		}
	}
	return aiCount == 4
}

// FlipBottomCard handles flipping a card from the bottom to determine dealer
// 翻底牌定庄（HTTP 兼容入口；现已主要由 GetTableGame tick 中按 3 秒/张自动驱动）
func FlipBottomCard(gameID string) (*GameTable, error) {
	table, err := GetTableGame(gameID)
	if err != nil {
		return nil, err
	}
	return flipNextBottomCardCore(table, gameID)
}

// flipNextBottomCardCore 翻开下一张底牌（无锁；调用方负责并发安全）
// 翻牌规则详见 rules/03-bidding.md §3.4 §3.5
func flipNextBottomCardCore(table *GameTable, gameID string) (*GameTable, error) {
	if table.Status != "calling" {
		return nil, fmt.Errorf("game not in calling phase")
	}

	if table.CallPhase != "flipping" {
		return nil, fmt.Errorf("not in flipping phase")
	}

	if len(table.FlippedBottomCards) >= len(table.BottomCards) {
		return nil, fmt.Errorf("all bottom cards have been flipped")
	}

	// 翻开下一张底牌
	nextCard := table.BottomCards[len(table.FlippedBottomCards)]
	table.FlippedBottomCards = append(table.FlippedBottomCards, nextCard)

	// 记录翻底牌日志
	LogGameAction(GameActionLogRequest{
		GameID:     gameID,
		ActionType: "flip_bottom",
		PlayerSeat: 0,
		PlayerID:   "",
		ActionData: map[string]interface{}{
			"card":          nextCard,
			"flipped_count": len(table.FlippedBottomCards),
			"total_bottom":  len(table.BottomCards),
		},
		ResultData: map[string]interface{}{
			"card_suit":  nextCard.Suit,
			"card_value": nextCard.Value,
		},
	})

	// 检查是否翻到了级牌
	rank := table.TrumpRank
	if nextCard.Value == rank {
		// 找到了级牌，确定庄家
		// 找出所有打这个级别的玩家，按逆时针顺序选择距离起始发牌人最近的
		var candidates []int
		for seat, hand := range table.PlayerHands {
			// 检查玩家的等级
			user, err := GetUserByID(hand.UserID)
			if err == nil && user.Level == rank {
				candidates = append(candidates, seat)
			}
		}

		if len(candidates) > 0 {
			// 按逆时针顺序找最近的
			selectedSeat := findClosestSeatCounterClockwise(table.StartingDealerSeat, candidates)
			table.DealerSeat = selectedSeat
			table.TrumpSuit = nextCard.Suit
			table.HostID = table.PlayerHands[selectedSeat].UserID
			// 切到 finished：保留翻底牌画面 3 秒（让最后一张翻牌动画完整播放），
			// 再由 GetTableGame tick 调用 finalizeDealerAndStartPlaying 推进到 discarding。
			table.CallPhase = "finished"
			table.UpdatedAt = time.Now()
			return table, nil
		}
	}

	// 如果翻完了所有底牌还没定庄，则首发人当庄
	if len(table.FlippedBottomCards) >= len(table.BottomCards) {
		table.DealerSeat = table.StartingDealerSeat

		// 从第7张底牌开始往前找，找第一张有花色的牌（非王）
		trumpSuit := ""
		for i := len(table.BottomCards) - 1; i >= 0; i-- {
			if table.BottomCards[i].Suit != "joker" && table.BottomCards[i].Suit != "" {
				// 找到了第一张有花色的牌
				trumpSuit = table.BottomCards[i].Suit
				break
			}
		}

		// 如果所有底牌都是王（极端情况），默认使用第一张底牌的花色或hearts
		if trumpSuit == "" {
			if len(table.BottomCards) > 0 && table.BottomCards[0].Suit != "joker" {
				trumpSuit = table.BottomCards[0].Suit
			} else {
				trumpSuit = "hearts" // 默认值
			}
		}

		table.TrumpSuit = trumpSuit
		table.HostID = table.PlayerHands[table.StartingDealerSeat].UserID
		// 切到 finished：同上，保留翻底牌画面 3 秒再 finalize。
		table.CallPhase = "finished"
		table.UpdatedAt = time.Now()
		return table, nil
	}

	table.UpdatedAt = time.Now()
	return table, nil
}

// findClosestSeatCounterClockwise finds the closest seat going counter-clockwise
// 从起始发牌人开始，按逆时针顺序找最近的玩家
func findClosestSeatCounterClockwise(startingSeat int, candidates []int) int {
	if len(candidates) == 0 {
		return startingSeat
	}
	if len(candidates) == 1 {
		return candidates[0]
	}

	// 按逆时针顺序检查（逆时针：1->5->4->3->2->1）
	// 也就是顺时针的反方向
	for i := 0; i < 5; i++ {
		seat := ((startingSeat - 1 - i + 5) % 5) + 1
		for _, candidate := range candidates {
			if candidate == seat {
				return candidate
			}
		}
	}

	return candidates[0]
}

// ensureDealerHasBottomCards 幂等地把底牌追加到庄家手牌中。
// 仅当庄家手牌尚未包含底牌（即数量等于发牌数量 31）时才追加，避免重复合入。
// 该函数被设计为"懒加载"步骤：进入扣底牌阶段（Status=="discarding"）后，
// 在 GetTableGame 或 DiscardBottomCards 等入口处按需调用，使前端在
// "定庄成功"展示窗口（仍处于 calling/finished）期间看到的庄家手牌保持 31 张，
// 只有真正进入扣底牌阶段后才显示 38 张。
func ensureDealerHasBottomCards(table *GameTable) {
	if table == nil || table.DealerSeat == 0 {
		return
	}
	dealerHand, ok := table.PlayerHands[table.DealerSeat]
	if !ok || dealerHand == nil {
		return
	}
	// 已经包含底牌（38 张）或底牌为空时无需处理
	if len(dealerHand.Cards) != 31 || len(table.BottomCards) == 0 {
		return
	}
	for _, card := range table.BottomCards {
		dealerHand.Cards = append(dealerHand.Cards, card)
	}
}

// finalizeDealerAndStartPlaying finalizes dealer selection and starts the playing phase
func finalizeDealerAndStartPlaying(table *GameTable) (*GameTable, error) {
	// 规则4.1-4.2：庄家流程
	// 1. 拿底牌：不在此处把底牌并入庄家手牌，避免在"定庄成功"展示窗口（仍处于
	//    Status=="calling" / CallPhase=="finished"）期间前端就看到庄家手牌
	//    多了 7 张。底牌将在 GetTableGame 进入 discarding 阶段时
	//    通过 ensureDealerHasBottomCards 懒加载并入庄家手牌。
	// 2. 扣回底牌（先扣7张牌）
	// 3. 叫朋友（扣牌完成后进行）
	table.Status = "discarding"
	table.CallPhase = "discarding"
	table.UpdatedAt = time.Now()

	LogGameAction(GameActionLogRequest{
		GameID:     table.GameID,
		ActionType: "dealer_settled",
		PlayerSeat: table.DealerSeat,
		ActionData: map[string]interface{}{
			"dealerSeat":         table.DealerSeat,
			"trumpSuit":          table.TrumpSuit,
			"trumpRank":          table.TrumpRank,
			"callRecords":        table.CallRecords,
			"passedSeats":        table.PassedSeats,
			"flippedBottomCards": table.FlippedBottomCards,
			"startingDealerSeat": table.StartingDealerSeat,
		},
		ResultData: map[string]interface{}{
			"status":    table.Status,
			"callPhase": table.CallPhase,
			"byFlip":    len(table.CallRecords) == 0,
		},
	})

	// 单人模式：如果庄家是AI，自动扣底并叫朋友
	if isSinglePlayerGame(table) && table.DealerSeat != 1 {
		// AI 庄家自动扣底前需要先把底牌并入手牌，使其能从 38 张里选 7 张扣回。
		ensureDealerHasBottomCards(table)
		// AI庄家自动扣底：选择最小的7张牌
		dealerHand, ok := table.PlayerHands[table.DealerSeat]
		if ok && dealerHand != nil && len(dealerHand.Cards) == 38 {
			// 找出最小的7张牌（按点数排序）
			cardValues := map[string]int{
				"2": 15, "A": 14, "K": 13, "Q": 12, "J": 11,
				"10": 10, "9": 9, "8": 8, "7": 7, "6": 6,
				"5": 5, "4": 4, "3": 3, "big": 17, "small": 16,
			}

			// 创建带索引的牌列表
			type indexedCard struct {
				index int
				value int
			}
			cards := make([]indexedCard, len(dealerHand.Cards))
			for i, card := range dealerHand.Cards {
				cards[i] = indexedCard{index: i, value: cardValues[card.Value]}
			}

			// 按点数排序（从小到大）
			sort.Slice(cards, func(i, j int) bool {
				return cards[i].value < cards[j].value
			})

			// 选择最小的7张牌的索引
			discardIndices := make([]int, 7)
			for i := 0; i < 7; i++ {
				discardIndices[i] = cards[i].index
			}

			// 自动扣底
			LogGameAction(GameActionLogRequest{
				GameID:     table.GameID,
				ActionType: "ai_auto_discard",
				PlayerSeat: table.DealerSeat,
				PlayerID:   dealerHand.UserID,
				ActionData: map[string]interface{}{
					"strategy":     "lowest7",
					"handBefore":   len(dealerHand.Cards),
					"discardIndex": discardIndices,
				},
				ResultData: map[string]interface{}{"dealerSeat": table.DealerSeat},
			})
			DiscardBottomCards(table.GameID, dealerHand.UserID, discardIndices)

			// 扣底完成后，自动叫朋友：黑桃A第1张
			// 重新获取table，因为DiscardBottomCards可能已经更新了状态
			table, _ = GetTableGame(table.GameID)
			dealerHand = table.PlayerHands[table.DealerSeat]
			if dealerHand != nil {
				_, err := CallFriendCard(table.GameID, dealerHand.UserID, "spades", "A", 1)
				if err != nil {
					// 如果叫黑桃A失败（可能在叫庄记录中），尝试叫红桃A
					_, err = CallFriendCard(table.GameID, dealerHand.UserID, "hearts", "A", 1)
					if err != nil {
						fmt.Printf("AI auto-call friend failed: %v\n", err)
						LogGameAction(GameActionLogRequest{
							GameID:     table.GameID,
							ActionType: "ai_call_friend_failed",
							PlayerSeat: table.DealerSeat,
							PlayerID:   dealerHand.UserID,
							ActionData: map[string]interface{}{"tried": []string{"spadesA", "heartsA"}},
							ResultData: map[string]interface{}{"error": err.Error()},
						})
					}
				}
			}
		}
	}

	return table, nil
}

// DiscardBottomCards 庄家扣牌（选择7张牌扣回底牌）
// 规则4.1：庄家在扣底完成后进入叫朋友阶段
func DiscardBottomCards(gameID string, userID string, cardIndices []int) (*GameTable, error) {
	table, err := GetTableGame(gameID)
	if err != nil {
		return nil, err
	}

	// 允许在calling_friend或discarding状态下扣牌
	// calling_friend：庄家刚叫完朋友，准备扣牌
	// discarding：向后兼容，实际应该统一使用calling_friend
	if table.Status != "calling_friend" && table.Status != "discarding" {
		return nil, fmt.Errorf("game not in calling_friend phase, current status: %s", table.Status)
	}

	// 兜底：底牌已经在 GetTableGame 进入 discarding 时懒加载并入庄家手牌；
	// 此处再幂等地确保一次，以防客户端直接调用本接口绕过 GetTableGame。
	ensureDealerHasBottomCards(table)

	// 验证只有庄家可以扣牌
	dealerHand, ok := table.PlayerHands[table.DealerSeat]
	if !ok || dealerHand.UserID != userID {
		return nil, fmt.Errorf("只有庄家可以扣牌")
	}

	// 验证选择了7张牌
	if len(cardIndices) != 7 {
		return nil, fmt.Errorf("必须选择7张牌扣底")
	}

	// 验证索引有效性
	for _, idx := range cardIndices {
		if idx < 0 || idx >= len(dealerHand.Cards) {
			return nil, fmt.Errorf("invalid card index: %d", idx)
		}
	}

	// 收集要扣的牌
	var discardedCards []Card
	usedIndices := make(map[int]bool)
	for _, idx := range cardIndices {
		if usedIndices[idx] {
			return nil, fmt.Errorf("duplicate card index: %d", idx)
		}
		usedIndices[idx] = true
		discardedCards = append(discardedCards, dealerHand.Cards[idx])
	}

	// 从庄家手牌中移除扣的牌
	newHandCards := make([]Card, 0, len(dealerHand.Cards)-7)
	for i, card := range dealerHand.Cards {
		if !usedIndices[i] {
			newHandCards = append(newHandCards, card)
		}
	}
	dealerHand.Cards = newHandCards
	table.PlayerHands[table.DealerSeat] = dealerHand

	// 将扣的牌放回底牌
	table.BottomCards = discardedCards

	// 记录扣牌日志
	LogGameAction(GameActionLogRequest{
		GameID:     gameID,
		ActionType: "discard_bottom",
		PlayerSeat: table.DealerSeat,
		PlayerID:   userID,
		ActionData: map[string]interface{}{
			"card_indices":    cardIndices,
			"discarded_cards": discardedCards,
		},
		ResultData: map[string]interface{}{
			"status":      "success",
			"dealer_seat": table.DealerSeat,
		},
	})

	// 扣牌完成后，进入叫朋友阶段
	// 规则：庄家扣底后需要叫朋友
	table.Status = "calling_friend"
	table.CallPhase = "calling_friend"
	table.UpdatedAt = time.Now()

	return table, nil
}

// ==================== 抠底相关函数 ====================

// BottomCardMultiplier represents the multiplier for bottom cards
type BottomCardMultiplier struct {
	Multiplier int    `json:"multiplier"`
	Reason     string `json:"reason"`
}

// CalculateBottomMultiplier calculates the multiplier for bottom cards based on the last trick
// 抠底倍数计算：
// - 单张抠底：×1
// - 对子抠底：×2
// - 三张抠底：×4
// - 拖拉机抠底：对应牌型翻倍
func CalculateBottomMultiplier(lastTrick []PlayedCard, trumpSuit string) BottomCardMultiplier {
	if len(lastTrick) == 0 {
		return BottomCardMultiplier{Multiplier: 0, Reason: "没有出牌"}
	}

	// 获取最后一轮的领出牌
	leadSeat := lastTrick[0].Seat
	var leadCards []Card
	for _, pc := range lastTrick {
		if pc.Seat == leadSeat {
			leadCards = append(leadCards, pc.Card)
		} else {
			break
		}
	}

	leadCount := len(leadCards)

	// 检查牌型
	if leadCount == 1 {
		return BottomCardMultiplier{Multiplier: 1, Reason: "单张抠底"}
	}

	if leadCount == 2 {
		// 检查是否是对子
		if leadCards[0].Value == leadCards[1].Value && leadCards[0].Suit == leadCards[1].Suit {
			return BottomCardMultiplier{Multiplier: 2, Reason: "对子抠底"}
		}
	}

	if leadCount == 3 {
		// 检查是否是三张
		if leadCards[0].Value == leadCards[1].Value && leadCards[1].Value == leadCards[2].Value &&
			leadCards[0].Suit == leadCards[1].Suit && leadCards[1].Suit == leadCards[2].Suit {
			return BottomCardMultiplier{Multiplier: 4, Reason: "三张抠底"}
		}
	}

	if leadCount >= 4 && leadCount%2 == 0 {
		// 检查是否是拖拉机（连对）
		if isValidTractor(leadCards) {
			pairCount := leadCount / 2
			return BottomCardMultiplier{Multiplier: pairCount, Reason: fmt.Sprintf("拖拉机抠底（%d对）", pairCount)}
		}
	}

	// 默认单张
	return BottomCardMultiplier{Multiplier: 1, Reason: "单张抠底"}
}

// isValidTractor checks if cards form a valid tractor (consecutive pairs or triples)
func isValidTractor(cards []Card) bool {
	if len(cards) < 4 {
		return false
	}

	// 检查是否同花色
	firstSuit := cards[0].Suit
	for _, card := range cards {
		if card.Suit != firstSuit {
			return false
		}
	}

	// Count occurrences of each value
	valueCounts := make(map[string]int)
	for _, card := range cards {
		valueCounts[card.Value]++
	}

	// Check if all values have the same count (either all 2s or all 3s)
	var expectedCount int
	firstValue := true
	for _, count := range valueCounts {
		if firstValue {
			expectedCount = count
			firstValue = false
			// Must be either 2 (pairs) or 3 (triples)
			if expectedCount != 2 && expectedCount != 3 {
				return false
			}
		} else {
			if count != expectedCount {
				return false
			}
		}
	}

	// Must have at least 2 groups
	if len(valueCounts) < 2 {
		return false
	}

	// 检查是否连续
	values := make([]string, 0, len(valueCounts))
	for value := range valueCounts {
		values = append(values, value)
	}

	sort.Slice(values, func(i, j int) bool {
		return getCardNumericValue(values[i]) < getCardNumericValue(values[j])
	})

	for i := 1; i < len(values); i++ {
		prevValue := getCardNumericValue(values[i-1])
		currValue := getCardNumericValue(values[i])
		if currValue != prevValue+1 {
			return false
		}
	}

	return true
}

// CalculateBottomCardsScore calculates the total score from bottom cards with multiplier
func CalculateBottomCardsScore(bottomCards []Card, multiplier int) int {
	if multiplier == 0 {
		return 0
	}

	totalScore := 0
	for _, card := range bottomCards {
		totalScore += getCardPoints(card)
	}

	return totalScore * multiplier
}

// AutoCallForDealer 自动为AI玩家叫庄（单人模式）
func AutoCallForDealer(gameID string) (*GameTable, error) {
	table, err := GetTableGame(gameID)
	if err != nil {
		return nil, err
	}

	if table.Status != "calling" && table.Status != "playing" {
		return nil, fmt.Errorf("game not in calling phase")
	}

	if table.TrumpSuit != "" {
		// Already has trump suit, skip
		return table, nil
	}

	// 在单人模式中，玩家1（人类）是庄家，直接使用人类玩家的最长花色作为主牌
	if hand, ok := table.PlayerHands[1]; ok {
		// 找出最长的花色
		suitCounts := make(map[string]int)
		for _, card := range hand.Cards {
			suitCounts[card.Suit]++
		}

		longestSuit := ""
		maxCount := 0
		for suit, count := range suitCounts {
			if count > maxCount && suit != "joker" {
				maxCount = count
				longestSuit = suit
			}
		}

		table.DealerSeat = 1
		table.TrumpSuit = longestSuit
		table.CallPhase = "finished"

		return finalizeDealerAndStartPlaying(table)
	}

	return nil, fmt.Errorf("human player not found")
}

// upgradeLevel upgrades a player's level by the specified number of levels
func upgradeLevel(currentLevel string, levelsUp int) string {
	if levelsUp <= 0 {
		return currentLevel
	}

	// Level progression: 2 -> 3 -> 4 -> 5 -> 6 -> 7 -> 8 -> 9 -> 10 -> J -> Q -> K -> A -> (win)
	levels := []string{"2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K", "A"}

	currentIndex := -1
	for i, level := range levels {
		if level == currentLevel {
			currentIndex = i
			break
		}
	}

	if currentIndex == -1 {
		return currentLevel
	}

	newIndex := currentIndex + levelsUp
	if newIndex >= len(levels) {
		return "A" // Max level
	}

	return levels[newIndex]
}

// ==================== 玩家准备相关函数 ====================

// PlayerReadyState 玩家准备状态
type PlayerReadyState struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Seat     int    `json:"seat"`
	IsReady  bool   `json:"isReady"`
}

// SetPlayerReady 设置玩家准备状态
func SetPlayerReady(gameID, userID string, isReady bool) error {
	query := `UPDATE game_players SET is_ready = $1 WHERE game_id = $2 AND user_id = $3`
	result, err := db.Exec(query, isReady, gameID, userID)
	if err != nil {
		return fmt.Errorf("failed to update ready status: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("player not found in game")
	}
	return nil
}

// GetPlayersReadyStatus 获取房间内所有玩家的准备状态
func GetPlayersReadyStatus(gameID string) ([]PlayerReadyState, error) {
	query := `
		SELECT gp.user_id, u.username, gp.seat_number, gp.is_ready
		FROM game_players gp
		JOIN users u ON gp.user_id = u.id
		WHERE gp.game_id = $1
		ORDER BY gp.seat_number`
	rows, err := db.Query(query, gameID)
	if err != nil {
		return nil, fmt.Errorf("failed to get ready status: %w", err)
	}
	defer rows.Close()

	var states []PlayerReadyState
	for rows.Next() {
		var state PlayerReadyState
		if err := rows.Scan(&state.UserID, &state.Username, &state.Seat, &state.IsReady); err != nil {
			return nil, fmt.Errorf("failed to scan ready status: %w", err)
		}
		states = append(states, state)
	}
	return states, nil
}

// AreAllPlayersReady 检查是否所有玩家都准备好了
func AreAllPlayersReady(gameID string) (bool, int, error) {
	query := `SELECT COUNT(*) FROM game_players WHERE game_id = $1 AND is_ready = FALSE`
	var notReadyCount int
	err := db.QueryRow(query, gameID).Scan(&notReadyCount)
	if err != nil {
		return false, 0, fmt.Errorf("failed to check ready status: %w", err)
	}

	// 获取总玩家数
	var totalPlayers int
	err = db.QueryRow(`SELECT COUNT(*) FROM game_players WHERE game_id = $1`, gameID).Scan(&totalPlayers)
	if err != nil {
		return false, 0, fmt.Errorf("failed to get player count: %w", err)
	}

	return notReadyCount == 0 && totalPlayers == 5, totalPlayers, nil
}

// ResetPlayersReady 重置所有玩家的准备状态
func ResetPlayersReady(gameID string) error {
	query := `UPDATE game_players SET is_ready = FALSE WHERE game_id = $1`
	_, err := db.Exec(query, gameID)
	if err != nil {
		return fmt.Errorf("failed to reset ready status: %w", err)
	}
	return nil
}
