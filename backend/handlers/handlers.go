package handlers

import (
	"fmt"
	"leve_up/middleware"
	"leve_up/models"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

var frontendDistDir = "../frontend/dist"

// SetFrontendDistDir sets frontend build output directory.
func SetFrontendDistDir(distDir string) {
	if distDir != "" {
		frontendDistDir = distDir
	}
}

// ServeFrontendIndex serves the SPA entry file.
func ServeFrontendIndex(c *gin.Context) {
	indexPath := filepath.Join(frontendDistDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Frontend build not found. Please run `cd frontend && npm run build` first.",
		})
		return
	}
	c.File(indexPath)
}

func parseCardIndices(data map[string]string, keys ...string) ([]int, error) {
	for _, key := range keys {
		raw := strings.TrimSpace(data[key])
		if raw == "" {
			continue
		}

		parts := strings.Split(raw, ",")
		result := make([]int, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			value, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid card index: %s", part)
			}
			result = append(result, value)
		}
		if len(result) > 0 {
			return result, nil
		}
	}
	return nil, fmt.Errorf("no card indices provided")
}

// Register handles user registration
func Register(c *gin.Context) {
	data, ok := middleware.ParseForm(c)
	if !ok {
		return
	}

	if errMsg, ok := middleware.RequireFields(data, []string{"username", "password"}); !ok {
		middleware.SendError(c, http.StatusBadRequest, errMsg)
		return
	}

	username := data["username"]
	password := data["password"]

	// Check password length
	if len(password) < 4 {
		middleware.SendError(c, http.StatusBadRequest, "Password must be at least 4 characters")
		return
	}

	// Create user
	user, err := models.CreateUser(username, password)
	if err == models.ErrUserExists {
		middleware.SendError(c, http.StatusConflict, "Username already exists")
		return
	}
	if err != nil {
		log.Println("CreateUser error:", err)
		middleware.SendError(c, http.StatusInternalServerError, "Failed to create user")
		return
	}

	// Generate token
	token, err := middleware.GenerateToken(user.ID)
	if err != nil {
		middleware.SendError(c, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	// Set token as cookie for page navigation
	c.SetSameSite(http.SameSiteDefaultMode)
	c.SetCookie("token", token, 86400, "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Registration successful",
		"token":   token,
		"user":    user,
	})
}

// Login handles user login
func Login(c *gin.Context) {
	data, ok := middleware.ParseForm(c)
	if !ok {
		return
	}

	if errMsg, ok := middleware.RequireFields(data, []string{"username", "password"}); !ok {
		middleware.SendError(c, http.StatusBadRequest, errMsg)
		return
	}

	username := data["username"]
	password := data["password"]

	// Find user
	user, err := models.GetUserByUsername(username)
	if err == models.ErrUserNotFound {
		middleware.SendError(c, http.StatusUnauthorized, "Invalid username or password")
		return
	}
	if err != nil {
		middleware.SendError(c, http.StatusInternalServerError, "Login failed")
		return
	}

	// Check password (in production, use bcrypt)
	if user.Password != password {
		middleware.SendError(c, http.StatusUnauthorized, "Invalid username or password")
		return
	}

	// Generate token
	token, err := middleware.GenerateToken(user.ID)
	if err != nil {
		middleware.SendError(c, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	// Set token as cookie for page navigation
	c.SetSameSite(http.SameSiteDefaultMode)
	c.SetCookie("token", token, 86400, "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Login successful",
		"token":   token,
		"user":    user,
	})
}

// Logout handles user logout
func Logout(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Logged out successfully",
	})
}

// GetCurrentUser returns the current logged-in user
func GetCurrentUser(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"user":    user,
	})
}

// CreateGame creates a new game room
func CreateGame(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)

	data, ok := middleware.ParseForm(c)
	if !ok {
		return
	}

	gameName := "新房间"
	if data["name"] != "" {
		gameName = data["name"]
	}

	game, err := models.CreateGame(gameName, user.ID)
	if err != nil {
		middleware.SendError(c, http.StatusInternalServerError, "Failed to create game")
		return
	}

	// WebSocket broadcast: notify all clients about new room
	if hub := GetWebSocketHub(); hub != nil {
		hub.BroadcastRoomCreated(game)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"game":    game,
		"gameId":  game.ID,
	})
}

// ListGames returns all active game rooms
func ListGames(c *gin.Context) {
	games, err := models.ListGames()
	if err != nil {
		middleware.SendError(c, http.StatusInternalServerError, "Failed to get games list")
		return
	}

	// Get player details for each game
	type GameWithPlayers struct {
		*models.GameState
		Players []map[string]interface{} `json:"players"`
	}

	result := make([]GameWithPlayers, 0, len(games))
	for _, game := range games {
		players := make([]map[string]interface{}, 0)
		for _, playerID := range game.PlayerIDs {
			if user, err := models.GetUserByID(playerID); err == nil {
				players = append(players, map[string]interface{}{
					"id":       user.ID,
					"username": user.Username,
					"level":    user.Level,
				})
			}
		}

		result = append(result, GameWithPlayers{
			GameState: game,
			Players:   players,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"games":   result,
	})
}

// GetGame retrieves a game by ID
func GetGame(c *gin.Context) {
	gameID := c.Param("id")

	game, err := models.GetGame(gameID)
	if err == models.ErrGameNotFound {
		middleware.SendError(c, http.StatusNotFound, "Game not found")
		return
	}
	if err != nil {
		middleware.SendError(c, http.StatusInternalServerError, "Failed to get game")
		return
	}

	// Get player details
	players := make([]map[string]interface{}, 0)
	for _, playerID := range game.PlayerIDs {
		if user, err := models.GetUserByID(playerID); err == nil {
			players = append(players, map[string]interface{}{
				"id":       user.ID,
				"username": user.Username,
				"level":    user.Level,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"game":    game,
		"players": players,
	})
}

// JoinGame adds a player to a game
func JoinGame(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	gameID := c.Param("id")

	err := models.JoinGame(gameID, user.ID)
	if err == models.ErrGameNotFound {
		middleware.SendError(c, http.StatusNotFound, "Game not found")
		return
	}
	if err == models.ErrGameFull {
		middleware.SendError(c, http.StatusConflict, "Game is full")
		return
	}
	if err != nil {
		middleware.SendError(c, http.StatusInternalServerError, "Failed to join game")
		return
	}

	game, _ := models.GetGame(gameID)

	// WebSocket broadcast: notify all clients about room update
	if hub := GetWebSocketHub(); hub != nil {
		hub.BroadcastRoomUpdate(game)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Joined game successfully",
		"game":    game,
	})
}

// PlayCard handles playing a card or multiple cards
func PlayCard(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	gameID := c.Param("id")

	data, ok := middleware.ParseForm(c)
	if !ok {
		return
	}

	cardIndices, err := parseCardIndices(data, "cardIndices", "cardIndex")
	if err != nil {
		middleware.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	result, err := models.PlayCardsGame(gameID, user.ID, cardIndices)
	if err != nil {
		fmt.Printf("PlayCardsGame error: %v, cardIndices: %v\n", err, cardIndices)
		middleware.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 出牌日志已在 models.PlayCardsGame 里记录（带完整牌面），这里不再重复记一条。

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"result":  result,
	})
}

// StartGameHandler starts a game (deals cards)
func StartGameHandler(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	gameID := c.Param("id")

	table, err := models.StartGame(gameID, user.ID)
	if err != nil {
		middleware.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	// WebSocket broadcast: notify all clients about room status change
	if hub := GetWebSocketHub(); hub != nil {
		game, _ := models.GetGame(gameID)
		if game != nil {
			hub.BroadcastRoomUpdate(game)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"table":   table,
		"message": "游戏开始，请亮庄",
	})
}

// CallFriendHandler handles the host calling a friend card
func CallFriendHandler(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	gameID := c.Param("id")

	data, ok := middleware.ParseForm(c)
	if !ok {
		return
	}

	suit := data["suit"]
	value := data["value"]
	positionStr := data["position"]

	if suit == "" || value == "" {
		middleware.SendError(c, http.StatusBadRequest, "suit and value are required")
		return
	}

	position := 1 // default
	if strings.TrimSpace(positionStr) != "" {
		if parsed, err := strconv.Atoi(strings.TrimSpace(positionStr)); err == nil {
			position = parsed
		}
	}

	table, err := models.CallFriendCard(gameID, user.ID, suit, value, position)
	if err != nil {
		middleware.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	// Log the call friend action
	game, _ := models.GetGame(gameID)
	if game != nil {
		playerSeat := 0
		for i, id := range game.PlayerIDs {
			if id == user.ID {
				playerSeat = i + 1
				break
			}
		}

		models.LogGameAction(models.GameActionLogRequest{
			GameID:     gameID,
			ActionType: "call_friend",
			PlayerSeat: playerSeat,
			PlayerID:   user.ID,
			ActionData: map[string]interface{}{
				"suit":     suit,
				"value":    value,
				"position": position,
			},
			ResultData: nil,
		})
	}

	// Build dealerTeam and scores from table data
	dealerTeam := make([]int, 0, 2)
	if table.DealerSeat > 0 {
		dealerTeam = append(dealerTeam, table.DealerSeat)
	}
	if table.FriendRevealed && table.FriendSeat > 0 {
		dealerTeam = append(dealerTeam, table.FriendSeat)
	}
	scores := make(map[int]int)
	for seat, hand := range table.PlayerHands {
		scores[seat] = hand.Score
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"table":   table,
		"game": map[string]interface{}{
			"id":             table.GameID,
			"status":         table.Status,
			"currentLevel":   table.CurrentLevel,
			"currentPlayer":  table.CurrentPlayer,
			"dealerTeam":     dealerTeam,
			"trumpSuit":      table.TrumpSuit,
			"trumpRank":      table.TrumpRank,
			"bottomCards":    table.BottomCards,
			"scores":         scores,
			"dealerSeat":     table.DealerSeat,
			"callRecords":    table.CallRecords,
			"passedSeats":    table.PassedSeats,
			"callPhase":      table.CallPhase,
			"hostCalledCard": table.HostCalledCard,
			"friendRevealed": table.FriendRevealed,
			"friendSeat":     table.FriendSeat,
		},
		"message": "Friend card called",
	})
}

// CallDealerHandler 处理玩家亮庄/反庄请求
func CallDealerHandler(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	gameID := c.Param("id")

	data, ok := middleware.ParseForm(c)
	if !ok {
		return
	}

	// suit 参数现在是可选的，如果不提供，将从选中的牌中自动提取
	suit := data["suit"]

	cardIndices, err := parseCardIndices(data, "cardIndices", "cardIndex")
	if err != nil {
		middleware.SendError(c, http.StatusBadRequest, "cardIndices is required")
		return
	}

	table, err := models.CallDealer(gameID, user.ID, suit, cardIndices)
	if err != nil {
		middleware.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	// Log the call dealer action
	game, _ := models.GetGame(gameID)
	if game != nil {
		playerSeat := 0
		for i, id := range game.PlayerIDs {
			if id == user.ID {
				playerSeat = i + 1
				break
			}
		}

		models.LogGameAction(models.GameActionLogRequest{
			GameID:     gameID,
			ActionType: "call_dealer",
			PlayerSeat: playerSeat,
			PlayerID:   user.ID,
			ActionData: map[string]interface{}{
				"suit":        suit,
				"cardIndices": cardIndices,
			},
			ResultData: table,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"table":   table,
		"message": "亮庄成功",
	})
}

// PassCallHandler handles a player passing on calling for dealer (不叫)
func PassCallHandler(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	gameID := c.Param("id")

	table, err := models.PassCall(gameID, user.ID)
	if err != nil {
		middleware.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"table":   table,
		"message": "不叫成功",
	})
}

// PlayerReadyHandler 处理玩家准备请求
func PlayerReadyHandler(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	gameID := c.Param("id")

	// 检查游戏状态
	game, err := models.GetGame(gameID)
	if err != nil {
		middleware.SendError(c, http.StatusNotFound, "Game not found")
		return
	}

	if game.Status != "waiting" {
		middleware.SendError(c, http.StatusBadRequest, "Game already started")
		return
	}

	// 设置玩家为准备状态
	err = models.SetPlayerReady(gameID, user.ID, true)
	if err != nil {
		middleware.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 获取准备状态列表
	readyStates, _ := models.GetPlayersReadyStatus(gameID)

	// 检查是否满足开始条件（5人全准备 或 3人全准备）
	allReady, totalPlayers, err := models.AreAllPlayersReady(gameID)
	if err == nil && allReady && (totalPlayers == 5 || totalPlayers >= 3) {
		// 自动开始游戏
		table, err := models.StartGame(gameID, game.HostID)
		if err == nil {
			// WebSocket广播游戏开始
			if hub := GetWebSocketHub(); hub != nil {
				hub.BroadcastMessage("game_started", map[string]interface{}{
					"gameId": gameID,
					"table":  table,
				})
			}

			c.JSON(http.StatusOK, gin.H{
				"success":      true,
				"message":      "游戏开始",
				"gameStarted":  true,
				"table":        table,
				"readyStates":  readyStates,
				"totalPlayers": totalPlayers,
			})
			return
		}
	}

	// WebSocket广播准备状态变化
	if hub := GetWebSocketHub(); hub != nil {
		hub.BroadcastRoomUpdate(game)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"message":      "已准备",
		"gameStarted":  false,
		"readyStates":  readyStates,
		"totalPlayers": totalPlayers,
	})
}

// PlayerCancelReadyHandler 处理玩家取消准备请求
func PlayerCancelReadyHandler(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	gameID := c.Param("id")

	// 检查游戏状态
	game, err := models.GetGame(gameID)
	if err != nil {
		middleware.SendError(c, http.StatusNotFound, "Game not found")
		return
	}

	if game.Status != "waiting" {
		middleware.SendError(c, http.StatusBadRequest, "Game already started")
		return
	}

	err = models.SetPlayerReady(gameID, user.ID, false)
	if err != nil {
		middleware.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 获取准备状态列表
	readyStates, _ := models.GetPlayersReadyStatus(gameID)
	_, totalPlayers, _ := models.AreAllPlayersReady(gameID)

	// WebSocket广播状态变化
	if hub := GetWebSocketHub(); hub != nil {
		hub.BroadcastRoomUpdate(game)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"message":      "已取消准备",
		"readyStates":  readyStates,
		"totalPlayers": totalPlayers,
	})
}

// GetReadyStatusHandler 获取房间准备状态
func GetReadyStatusHandler(c *gin.Context) {
	gameID := c.Param("id")

	readyStates, err := models.GetPlayersReadyStatus(gameID)
	if err != nil {
		middleware.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	allReady, totalPlayers, _ := models.AreAllPlayersReady(gameID)

	c.JSON(http.StatusOK, gin.H{
		"success":      true,
		"readyStates":  readyStates,
		"totalPlayers": totalPlayers,
		"allReady":     allReady,
	})
}

// DealNextCardHandler 发下一轮牌
func DealNextCardHandler(c *gin.Context) {
	gameID := c.Param("id")

	table, complete, err := models.DealNextCard(gameID)
	if err != nil {
		middleware.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	// WebSocket广播发牌状态
	if hub := GetWebSocketHub(); hub != nil {
		hub.BroadcastMessage("deal_cards", map[string]interface{}{
			"gameId":              gameID,
			"dealtCardCount":      table.DealtCardCount,
			"totalCardsPerPlayer": table.TotalCardsPerPlayer,
			"complete":            complete,
			"dealerSeat":          table.DealerSeat,
			"trumpSuit":           table.TrumpSuit,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success":             true,
		"table":               table,
		"complete":            complete,
		"dealtCardCount":      table.DealtCardCount,
		"totalCardsPerPlayer": table.TotalCardsPerPlayer,
	})
}

// CheckCountdownHandler 检查并处理倒计时
func CheckCountdownHandler(c *gin.Context) {
	gameID := c.Param("id")

	table, err := models.CheckAndProcessCountdown(gameID)
	if err != nil {
		middleware.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"table":   table,
	})
}

// FlipBottomCardHandler handles flipping a card from the bottom
func FlipBottomCardHandler(c *gin.Context) {
	gameID := c.Param("id")

	table, err := models.FlipBottomCard(gameID)
	if err != nil {
		middleware.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"table":   table,
	})
}

// DiscardBottomCardsHandler handles the dealer discarding cards to the bottom
func DiscardBottomCardsHandler(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	gameID := c.Param("id")

	data, ok := middleware.ParseForm(c)
	if !ok {
		return
	}

	cardIndices, err := parseCardIndices(data, "cardIndices", "cardIndex")
	if err != nil {
		middleware.SendError(c, http.StatusBadRequest, "cardIndices is required")
		return
	}

	table, err := models.DiscardBottomCards(gameID, user.ID, cardIndices)
	if err != nil {
		middleware.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	// 扣底日志已在 models.DiscardBottomCards 里记录（带完整牌面与牌桌快照），这里不再重复记一条。

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"table":   table,
		"message": "扣牌成功",
	})
}

// CreateSinglePlayerGame creates a single player game with AI opponents
func CreateSinglePlayerGame(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)

	data, ok := middleware.ParseForm(c)
	if !ok {
		return
	}

	gameName := "单人练习"
	if data["name"] != "" {
		gameName = data["name"]
	}

	game, err := models.CreateSinglePlayerGame(gameName, user.ID)
	if err != nil {
		log.Println("CreateSinglePlayerGame error:", err)
		middleware.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"game":    game,
		"gameId":  game.ID,
	})
}

// StartSinglePlayerGame starts a single player game and auto-plays AI
func StartSinglePlayerGame(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	gameID := c.Param("id")

	table, err := models.StartSinglePlayerGame(gameID, user.ID)
	if err != nil {
		middleware.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	// StartSinglePlayerGame now handles auto-calling and AI counter-calling internally

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"table":   table,
	})
}

// PassTurnHandler handles a player passing their turn (不出)
func PassTurnHandler(c *gin.Context) {
	user, _ := middleware.GetCurrentUser(c)
	gameID := c.Param("id")

	result, err := models.PassTurn(gameID, user.ID)
	if err != nil {
		middleware.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	// Log the pass action
	game, _ := models.GetGame(gameID)
	if game != nil {
		playerSeat := 0
		for i, id := range game.PlayerIDs {
			if id == user.ID {
				playerSeat = i + 1
				break
			}
		}

		models.LogGameAction(models.GameActionLogRequest{
			GameID:     gameID,
			ActionType: "pass",
			PlayerSeat: playerSeat,
			PlayerID:   user.ID,
			ActionData: nil,
			ResultData: result,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"result":  result,
		"message": "Pass successful",
	})
}

// AIPlayHandler makes AI players play automatically
func AIPlayHandler(c *gin.Context) {
	gameID := c.Param("id")

	table, err := models.AIPlayTurn(gameID)
	if err != nil {
		middleware.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"table":   table,
	})
}

// GetGameTableHandler returns the current game table state
func GetGameTableHandler(c *gin.Context) {
	gameID := c.Param("id")
	user, _ := middleware.GetCurrentUser(c)

	table, err := models.GetTableGame(gameID)
	if err != nil {
		middleware.SendError(c, http.StatusNotFound, err.Error())
		return
	}

	players := make([]map[string]interface{}, 0)
	scores := make(map[int]int)
	myPosition := 0
	myHand := make([]models.Card, 0)

	// 如果游戏还在等待状态，从数据库获取玩家列表
	if table.Status == "waiting" {
		game, err := models.GetGame(gameID)
		// 获取所有玩家的准备状态
		readyStates, _ := models.GetPlayersReadyStatus(gameID)
		readyMap := make(map[string]bool)
		for _, rs := range readyStates {
			readyMap[rs.UserID] = rs.IsReady
		}

		if err == nil {
			for i, playerID := range game.PlayerIDs {
				var username string
				var isAI bool

				var level string = "2" // 默认等级
				if models.IsAIUserID(playerID) {
					isAI = true
					username = fmt.Sprintf("AI-%s", models.AISeatLabel(playerID))
				} else if u, err := models.GetUserByID(playerID); err == nil {
					username = u.Username
					level = u.Level
				} else {
					username = fmt.Sprintf("玩家%d", i+1)
				}

				// 从准备状态映射中获取真实的准备状态
				isReady := readyMap[playerID]

				playerInfo := map[string]interface{}{
					"id":        i + 1,
					"userId":    playerID,
					"position":  i + 1,
					"username":  username,
					"isReady":   isReady,
					"isAI":      isAI,
					"cardCount": 0,
					"isFriend":  false,
					"level":     level,
				}
				players = append(players, playerInfo)

				if playerID == user.ID {
					myPosition = i + 1
				}
			}
		}
	} else {
		// 游戏已开始，从 PlayerHands 获取信息
		for seat := 1; seat <= 5; seat++ {
			if hand, ok := table.PlayerHands[seat]; ok {
				username := fmt.Sprintf("玩家%d", seat)
				level := "2" // 默认等级
				isAI := models.IsAIUserID(hand.UserID)
				if isAI {
					username = fmt.Sprintf("AI-%d", seat)
				} else if u, err := models.GetUserByID(hand.UserID); err == nil {
					username = u.Username
					level = u.Level
				}

				playerInfo := map[string]interface{}{
					"id":        seat,
					"userId":    hand.UserID,
					"position":  seat,
					"username":  username,
					"isReady":   table.Status != "waiting",
					"isAI":      isAI,
					"cardCount": len(hand.Cards),
					"isFriend":  hand.IsFriend,
					"level":     level,
				}
				players = append(players, playerInfo)

				scores[seat] = hand.Score
				if hand.UserID == user.ID {
					myPosition = seat
					myHand = hand.Cards
				}
			}
		}
	}

	// 转换 currentTrick：将同一玩家连续出的牌合并为一个 entry，并正确设置 isLead
	currentTrick := make([]map[string]interface{}, 0)
	if len(table.CurrentTrick) > 0 {
		// 找出第一张有 IsLead=true 的 entry index（领牌玩家的起始位置）
		leadStart := -1
		for i, trick := range table.CurrentTrick {
			if trick.IsLead {
				leadStart = i
				break
			}
		}
		// 按玩家分组：同一个玩家的连续 entries 合并
		groups := []map[string]interface{}{}
		i := 0
		for i < len(table.CurrentTrick) {
			seat := table.CurrentTrick[i].Seat
			cards := []models.Card{}
			for i < len(table.CurrentTrick) && table.CurrentTrick[i].Seat == seat {
				cards = append(cards, table.CurrentTrick[i].Card)
				i++
			}
			groups = append(groups, map[string]interface{}{
				"playerId": seat,
				"cards":    cards,
			})
		}
		// 标记领牌者（leadStart 位置）
		if leadStart >= 0 && leadStart < len(groups) {
			groups[leadStart]["isLead"] = true
		}
		currentTrick = groups
	}

	// 转换上一轮完成的出牌记录
	lastCompletedTrick := make([]map[string]interface{}, 0, len(table.LastCompletedTrick))
	for _, trick := range table.LastCompletedTrick {
		lastCompletedTrick = append(lastCompletedTrick, map[string]interface{}{
			"playerId": trick.Seat,
			"cards":    []models.Card{trick.Card},
		})
	}

	dealerTeam := make([]int, 0, 2)
	if table.DealerSeat > 0 {
		dealerTeam = append(dealerTeam, table.DealerSeat)
	}
	if table.FriendRevealed && table.FriendSeat > 0 {
		dealerTeam = append(dealerTeam, table.FriendSeat)
	}

	var trumpSuit interface{}
	if table.TrumpSuit != "" {
		trumpSuit = table.TrumpSuit
	}

	gamePayload := map[string]interface{}{
		"id":                 table.GameID,
		"status":             table.Status,
		"currentLevel":       table.CurrentLevel,
		"currentPlayer":      table.CurrentPlayer,
		"dealerTeam":         dealerTeam,
		"currentTrick":       currentTrick,
		"lastCompletedTrick": lastCompletedTrick,
		"players":            players,
		"myHand":             myHand,
		"myPosition":         myPosition,
		"trumpSuit":          trumpSuit,
		"trumpRank":          table.TrumpRank,
		"bottomCards":        table.BottomCards,
		"flippedBottomCards": table.FlippedBottomCards,
		"scores":             scores,
		"dealerSeat":         table.DealerSeat,
		"callRecords":        table.CallRecords,
		"passedSeats":        table.PassedSeats,
		"callCountdown":      table.CallCountdown,
		"callPhase":          table.CallPhase,
		"hostCalledCard":     table.HostCalledCard,
		"friendRevealed":     table.FriendRevealed,
		"friendSeat":         table.FriendSeat,
		// 甩牌失败高亮显示
		"throwBlocker":     table.ThrowBlocker,
		"throwBlockerCard": table.ThrowBlockerCard,
		// 本局结算信息
		"totalPoints":             table.TotalPoints,
		"roundResults":            table.RoundResults,
		"nextRoundCountdownStart": table.NextRoundCountdownStart,
		// 发牌阶段进度（供前端动画使用）
		"dealtCardCount":      table.DealtCardCount,
		"totalCardsPerPlayer": table.TotalCardsPerPlayer,
		"dealingPhase":        table.DealingPhase,
		"lastDealtSeat":       table.LastDealtSeat,
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"game":    gamePayload,
		// Backward-compatible fields
		"gameId":         table.GameID,
		"status":         table.Status,
		"currentLevel":   table.CurrentLevel,
		"trumpSuit":      table.TrumpSuit,
		"hostCalledCard": table.HostCalledCard,
		"friendRevealed": table.FriendRevealed,
		"friendSeat":     table.FriendSeat,
		"currentPlayer":  table.CurrentPlayer,
		"currentTrick":   table.CurrentTrick,
		"lastPlay":       table.LastPlay,
		"players":        players,
		"myHand":         myHand,
	})
}

// Page handlers

func IndexHandler(c *gin.Context) {
	renderTemplatePage(c, "index.html", "找朋友升级 - 首页", nil)
}

// tryGetUserFromToken attempts to get user from JWT token
func tryGetUserFromToken(tokenString string) (*models.User, bool) {
	claims := &middleware.Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return middleware.GetJWTSecret(), nil
	})

	if err != nil || !token.Valid {
		return nil, false
	}

	user, err := models.GetUserByID(claims.UserID)
	if err != nil {
		return nil, false
	}

	return user, true
}

// resolveOptionalUser tries to resolve user from context, header, or cookie.
func resolveOptionalUser(c *gin.Context) (*models.User, bool) {
	if user, ok := middleware.GetCurrentUser(c); ok {
		return user, true
	}

	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && parts[0] == "Bearer" {
			if user, ok := tryGetUserFromToken(parts[1]); ok {
				c.Set("user", user)
				c.Set("userID", user.ID)
				return user, true
			}
		}
	}

	cookieToken, err := c.Cookie("token")
	if err == nil && cookieToken != "" {
		if user, ok := tryGetUserFromToken(cookieToken); ok {
			c.Set("user", user)
			c.Set("userID", user.ID)
			return user, true
		}
	}

	return nil, false
}

func renderTemplatePage(c *gin.Context, templateName, title string, data gin.H) {
	_ = templateName
	_ = title
	_ = data
	ServeFrontendIndex(c)
}

func LoginPageHandler(c *gin.Context) {
	renderTemplatePage(c, "login.html", "登录 - 找朋友升级", nil)
}

func RegisterPageHandler(c *gin.Context) {
	renderTemplatePage(c, "register.html", "注册 - 找朋友升级", nil)
}

func RulesPageHandler(c *gin.Context) {
	renderTemplatePage(c, "rules.html", "游戏规则 - 找朋友升级", nil)
}

func GamePageHandler(c *gin.Context) {
	renderTemplatePage(c, "game.html", "游戏大厅 - 找朋友升级", nil)
}

func GameTablePageHandler(c *gin.Context) {
	ServeFrontendIndex(c)
}

// SinglePlayerGamePageHandler handles single player game page
func SinglePlayerGamePageHandler(c *gin.Context) {
	ServeFrontendIndex(c)
}

// ReplayPageHandler handles game replay page
func ReplayPageHandler(c *gin.Context) {
	ServeFrontendIndex(c)
}

// GetGameReplayHandler retrieves the replay data for a specific game
func GetGameReplayHandler(c *gin.Context) {
	gameID := c.Param("id")

	replay, err := models.GetGameReplay(gameID)
	if err != nil {
		middleware.SendError(c, http.StatusNotFound, "Replay not found for this game")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"replay":  replay,
	})
}

// GetGameActionsHandler retrieves all action logs for a specific game
func GetGameActionsHandler(c *gin.Context) {
	gameID := c.Param("id")

	actions, err := models.GetGameActionLogs(gameID)
	if err != nil {
		middleware.SendError(c, http.StatusInternalServerError, "Failed to retrieve game actions")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"actions": actions,
		"count":   len(actions),
	})
}

// GetPlayerPlayedCardsHandler 获取玩家在当前游戏中的出牌历史
func GetPlayerPlayedCardsHandler(c *gin.Context) {
	gameID := c.Param("id")
	userID := c.GetString("userID")

	// 获取游戏信息，找到玩家的座位号
	table, err := models.GetTableGame(gameID)
	if err != nil {
		middleware.SendError(c, http.StatusNotFound, "Game not found")
		return
	}

	// 找到玩家的座位号
	var playerSeat int
	found := false
	for seat, hand := range table.PlayerHands {
		if hand.UserID == userID {
			playerSeat = seat
			found = true
			break
		}
	}

	if !found {
		middleware.SendError(c, http.StatusForbidden, "Player not in game")
		return
	}

	// 获取该玩家的所有出牌历史
	playedCards, err := models.GetPlayerPlayedCardsHistory(gameID, playerSeat)
	if err != nil {
		middleware.SendError(c, http.StatusInternalServerError, "Failed to retrieve played cards history")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"playedCards": playedCards,
		"count":       len(playedCards),
		"playerSeat":  playerSeat,
	})
}

// NextRoundHandler 开始新的一局
func NextRoundHandler(c *gin.Context) {
	gameID := c.Param("id")

	table, err := models.NextRound(gameID)
	if err != nil {
		middleware.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	// WebSocket 广播新一局开始
	game, _ := models.GetGame(gameID)
	if hub := GetWebSocketHub(); hub != nil && game != nil {
		hub.BroadcastRoomUpdate(game)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"table":   table,
		"message": "新一局开始",
	})
}
