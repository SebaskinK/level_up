package models

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
)

// User represents a user in the system
type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Password  string    `json:"password"`
	Level     string    `json:"level"`
	Wins      int       `json:"wins"`
	Losses    int       `json:"losses"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// LevelOrder defines the order of levels from lowest to highest
var LevelOrder = []string{"2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K", "A"}

// generateID generates a unique ID
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// CreateUser creates a new user
func CreateUser(username, password string) (*User, error) {
	id := generateID()

	query := `INSERT INTO users (id, username, password, level, wins, losses) VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := db.Exec(query, id, username, password, "2", 0, 0)
	if err != nil {
		// Check for duplicate username
		if isDuplicateError(err) {
			return nil, ErrUserExists
		}
		return nil, err
	}

	return GetUserByID(id)
}

// GetUserByUsername retrieves a user by username
func GetUserByUsername(username string) (*User, error) {
	query := `SELECT id, username, password, level, wins, losses, created_at, updated_at FROM users WHERE username = $1`

	user := &User{}
	err := db.QueryRow(query, username).Scan(
		&user.ID, &user.Username, &user.Password, &user.Level,
		&user.Wins, &user.Losses, &user.CreatedAt, &user.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetUserByID retrieves a user by ID
func GetUserByID(id string) (*User, error) {
	query := `SELECT id, username, password, level, wins, losses, created_at, updated_at FROM users WHERE id = $1`

	user := &User{}
	err := db.QueryRow(query, id).Scan(
		&user.ID, &user.Username, &user.Password, &user.Level,
		&user.Wins, &user.Losses, &user.CreatedAt, &user.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	return user, nil
}

// UpdateUserLevel updates a user's level
func UpdateUserLevel(userID string, newLevel string) error {
	query := `UPDATE users SET level = $1 WHERE id = $2`
	_, err := db.Exec(query, newLevel, userID)
	return err
}

// IncrementUserWins increments a user's win count
func IncrementUserWins(userID string) error {
	query := `UPDATE users SET wins = wins + 1 WHERE id = $1`
	_, err := db.Exec(query, userID)
	return err
}

// IncrementUserLosses increments a user's loss count
func IncrementUserLosses(userID string) error {
	query := `UPDATE users SET losses = losses + 1 WHERE id = $1`
	_, err := db.Exec(query, userID)
	return err
}

// GetNextLevel returns the next level after the current one
func GetNextLevel(currentLevel string) string {
	return GetLevelAfter(currentLevel, 1)
}

// GetLevelAfter returns the level after advancing count levels
func GetLevelAfter(currentLevel string, count int) string {
	currentIndex := -1
	for i, level := range LevelOrder {
		if level == currentLevel {
			currentIndex = i
			break
		}
	}

	if currentIndex == -1 {
		return currentLevel
	}

	// 升过 A 之后从 2 接着往上循环，不截断在 A
	return LevelOrder[(currentIndex+count)%len(LevelOrder)]
}

// isDuplicateError checks if the error is a duplicate key error
func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	// Check PostgreSQL error code 23505 (unique_violation)
	if pqErr, ok := err.(*pq.Error); ok {
		return pqErr.Code == "23505"
	}
	// Fallback to string matching for other databases
	errMsg := err.Error()
	return contains(errMsg, "Duplicate entry") ||
		contains(errMsg, "duplicate key") ||
		contains(errMsg, "unique constraint") ||
		contains(errMsg, "23505")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// ListUsers returns all users
func ListUsers() ([]*User, error) {
	query := `SELECT id, username, password, level, wins, losses, created_at, updated_at FROM users ORDER BY created_at DESC`

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		user := &User{}
		err := rows.Scan(
			&user.ID, &user.Username, &user.Password, &user.Level,
			&user.Wins, &user.Losses, &user.CreatedAt, &user.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	return users, rows.Err()
}
