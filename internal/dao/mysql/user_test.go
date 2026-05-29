package mysql

import (
	"fmt"
	"testing"
	"time"
)

func TestGetUserByIdHandlesNullEmail(t *testing.T) {
	userID := time.Now().UnixNano()
	username := fmt.Sprintf("null_email_%d", userID)

	_, err := db.Exec(
		`insert into user(user_id, username, password, email, gender) values (?, ?, ?, null, 0)`,
		userID,
		username,
		"password",
	)
	if err != nil {
		t.Fatalf("insert test user failed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`delete from user where user_id = ?`, userID)
	})

	user, err := GetUserById(userID)
	if err != nil {
		t.Fatalf("GetUserById should handle null email: %v", err)
	}
	if user.Email != "" {
		t.Fatalf("expected null email to be returned as empty string, got %q", user.Email)
	}
}
