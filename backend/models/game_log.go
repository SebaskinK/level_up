package models

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// GameActionLog represents a single action in a game
type GameActionLog struct {
	ID         int             `json:"id"`
	GameID     string          `json:"gameId"`
	ActionType string          `json:"actionType"`
	PlayerSeat int             `json:"playerSeat"`
	PlayerID   string          `json:"playerId"`
	ActionData json.RawMessage `json:"actionData"`
	ResultData json.RawMessage `json:"resultData"`
	Timestamp  time.Time       `json:"timestamp"`
}

// GameReplay represents complete game replay data
type GameReplay struct {
	ID              int             `json:"id"`
	GameID          string          `json:"gameId"`
	InitialState    json.RawMessage `json:"initialState"`
	FinalState      json.RawMessage `json:"finalState"`
	TotalActions    int             `json:"totalActions"`
	DurationSeconds int             `json:"durationSeconds"`
	WinnerTeam      string          `json:"winnerTeam"`
	FinalScore      int             `json:"finalScore"`
	CreatedAt       time.Time       `json:"createdAt"`
}

// GameActionLogRequest represents the data for logging a game action
type GameActionLogRequest struct {
	GameID     string      `json:"gameId"`
	ActionType string      `json:"actionType"`
	PlayerSeat int         `json:"playerSeat"`
	PlayerID   string      `json:"playerId"`
	ActionData interface{} `json:"actionData"`
	ResultData interface{} `json:"resultData"`
}

// LogGameAction logs a game action to the database
func LogGameAction(req GameActionLogRequest) error {
	actionDataJSON, err := json.Marshal(req.ActionData)
	if err != nil {
		return fmt.Errorf("failed to marshal action data: %w", err)
	}

	var resultDataJSON []byte
	if req.ResultData != nil {
		resultDataJSON, err = json.Marshal(req.ResultData)
		if err != nil {
			return fmt.Errorf("failed to marshal result data: %w", err)
		}
	}

	// player_id 有外键约束指向 users，空串不是合法用户 ID，必须转成 NULL
	var playerID interface{}
	if req.PlayerID != "" {
		playerID = req.PlayerID
	}

	query := `
		INSERT INTO game_action_logs (game_id, action_type, player_seat, player_id, action_data, result_data)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err = db.Exec(query, req.GameID, req.ActionType, req.PlayerSeat, playerID, actionDataJSON, resultDataJSON)
	if err != nil {
		// 日志写失败不能拖垮对局，但必须留痕，否则会像之前那样静默丢数据
		log.Printf("[game-log] 写入失败 game=%s type=%s seat=%d: %v",
			req.GameID, req.ActionType, req.PlayerSeat, err)
		return fmt.Errorf("failed to insert game action log: %w", err)
	}

	return nil
}

// GetPlayerPlayedCardsHistory 获取特定玩家在某个游戏中的所有出牌历史
func GetPlayerPlayedCardsHistory(gameID string, playerSeat int) ([]GameActionLog, error) {
	query := `
		SELECT id, game_id, action_type, player_seat, player_id, action_data, result_data, timestamp
		FROM game_action_logs
		WHERE game_id = $1 AND action_type = 'play_cards' AND player_seat = $2
		ORDER BY timestamp ASC
	`

	rows, err := db.Query(query, gameID, playerSeat)
	if err != nil {
		return nil, fmt.Errorf("failed to query player played cards history: %w", err)
	}
	defer rows.Close()

	var logs []GameActionLog
	for rows.Next() {
		var log GameActionLog
		var playerID sql.NullString
		var resultData sql.NullString

		err := rows.Scan(
			&log.ID,
			&log.GameID,
			&log.ActionType,
			&log.PlayerSeat,
			&playerID,
			&log.ActionData,
			&resultData,
			&log.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan game action log: %w", err)
		}

		if playerID.Valid {
			log.PlayerID = playerID.String
		}
		if resultData.Valid {
			log.ResultData = json.RawMessage(resultData.String)
		}

		logs = append(logs, log)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating game action logs: %w", err)
	}

	return logs, nil
}

// GetGameActionLogs retrieves all action logs for a specific game
func GetGameActionLogs(gameID string) ([]GameActionLog, error) {
	query := `
		SELECT id, game_id, action_type, player_seat, player_id, action_data, result_data, timestamp
		FROM game_action_logs
		WHERE game_id = $1
		ORDER BY timestamp ASC
	`

	rows, err := db.Query(query, gameID)
	if err != nil {
		return nil, fmt.Errorf("failed to query game action logs: %w", err)
	}
	defer rows.Close()

	var logs []GameActionLog
	for rows.Next() {
		var log GameActionLog
		var playerID sql.NullString
		var resultData sql.NullString

		err := rows.Scan(
			&log.ID,
			&log.GameID,
			&log.ActionType,
			&log.PlayerSeat,
			&playerID,
			&log.ActionData,
			&resultData,
			&log.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan game action log: %w", err)
		}

		if playerID.Valid {
			log.PlayerID = playerID.String
		}
		if resultData.Valid {
			log.ResultData = json.RawMessage(resultData.String)
		}

		logs = append(logs, log)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating game action logs: %w", err)
	}

	return logs, nil
}

// CreateGameReplay creates a replay record for a completed game
func CreateGameReplay(gameID string, initialState, finalState interface{}, totalActions, durationSeconds int, winnerTeam string, finalScore int) error {
	initialStateJSON, err := json.Marshal(initialState)
	if err != nil {
		return fmt.Errorf("failed to marshal initial state: %w", err)
	}

	finalStateJSON, err := json.Marshal(finalState)
	if err != nil {
		return fmt.Errorf("failed to marshal final state: %w", err)
	}

	query := `
		INSERT INTO game_replays (game_id, initial_state, final_state, total_actions, duration_seconds, winner_team, final_score)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (game_id) DO UPDATE SET
			final_state = EXCLUDED.final_state,
			total_actions = EXCLUDED.total_actions,
			duration_seconds = EXCLUDED.duration_seconds,
			winner_team = EXCLUDED.winner_team,
			final_score = EXCLUDED.final_score
	`

	_, err = db.Exec(query, gameID, initialStateJSON, finalStateJSON, totalActions, durationSeconds, winnerTeam, finalScore)
	if err != nil {
		return fmt.Errorf("failed to insert/update game replay: %w", err)
	}

	return nil
}

// GetGameReplay retrieves the replay data for a specific game
func GetGameReplay(gameID string) (*GameReplay, error) {
	query := `
		SELECT id, game_id, initial_state, final_state, total_actions, duration_seconds, winner_team, final_score, created_at
		FROM game_replays
		WHERE game_id = $1
	`

	var replay GameReplay
	var winnerTeam sql.NullString

	err := db.QueryRow(query, gameID).Scan(
		&replay.ID,
		&replay.GameID,
		&replay.InitialState,
		&replay.FinalState,
		&replay.TotalActions,
		&replay.DurationSeconds,
		&winnerTeam,
		&replay.FinalScore,
		&replay.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("replay not found for game %s", gameID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query game replay: %w", err)
	}

	if winnerTeam.Valid {
		replay.WinnerTeam = winnerTeam.String
	}

	return &replay, nil
}

// GetAllGameReplays retrieves all game replays (for history page)
func GetAllGameReplays(limit, offset int) ([]GameReplay, error) {
	query := `
		SELECT id, game_id, initial_state, final_state, total_actions, duration_seconds, winner_team, final_score, created_at
		FROM game_replays
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := db.Query(query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query game replays: %w", err)
	}
	defer rows.Close()

	var replays []GameReplay
	for rows.Next() {
		var replay GameReplay
		var winnerTeam sql.NullString

		err := rows.Scan(
			&replay.ID,
			&replay.GameID,
			&replay.InitialState,
			&replay.FinalState,
			&replay.TotalActions,
			&replay.DurationSeconds,
			&winnerTeam,
			&replay.FinalScore,
			&replay.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan game replay: %w", err)
		}

		if winnerTeam.Valid {
			replay.WinnerTeam = winnerTeam.String
		}

		replays = append(replays, replay)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating game replays: %w", err)
	}

	return replays, nil
}

// ListRecentPlayedGames 返回最近若干局「有 playing_start 快照、可供重放」的对局 ID。
func ListRecentPlayedGames(limit int) ([]string, error) {
	rows, err := db.Query(`
		SELECT DISTINCT game_id
		FROM game_action_logs
		WHERE action_type = 'playing_start'
		ORDER BY game_id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
