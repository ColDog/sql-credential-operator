package db

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func suite(t *testing.T, activeUser string, db DB) {
	ctx := context.Background()

	t.Run("CreateUser", func(t *testing.T) {
		err := db.CreateUser(ctx, User{
			Name:     "test",
			Password: "password",
			Role:     "pg_monitor",
		})
		require.NoError(t, err)

		err = db.CreateUser(ctx, User{
			Name:     "test",
			Password: "password",
			Role:     "pg_monitor",
		})
		require.NoError(t, err)
	})

	t.Run("RemoveUser", func(t *testing.T) {
		err := db.RemoveUser(ctx, "nouser")
		require.NoError(t, err)

		err = db.RemoveUser(ctx, "test")
		require.NoError(t, err)

		err = db.RemoveUser(ctx, "test")
		require.NoError(t, err)
	})

	t.Run("IsActive", func(t *testing.T) {
		active, err := db.IsActive(ctx, "test")
		require.NoError(t, err)
		require.False(t, active)

		active, err = db.IsActive(ctx, "nonexistent")
		require.NoError(t, err)
		require.False(t, active)

		active, err = db.IsActive(ctx, activeUser)
		require.NoError(t, err)
		require.True(t, active)
	})
}

func Test_PG(t *testing.T) {
	db, err := New("postgres", "postgres://postgres@127.0.0.1:5432/default?sslmode=disable")
	defer db.Close()

	require.NoError(t, err)

	suite(t, "postgres", db)
}
