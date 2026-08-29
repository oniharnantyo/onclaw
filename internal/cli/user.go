package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/oniharnantyo/onclaw/internal/auth"
	"github.com/oniharnantyo/onclaw/internal/config"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/storage"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/urfave/cli/v3"
)

const maxAvatarSize = 2 * 1024 * 1024 // 2MB

// userCmd handles user management CLI commands.
type userCmd struct {
	hasher auth.PasswordHasher
}

// NewUserCmd creates a new userCmd instance.
func NewUserCmd() *userCmd {
	return NewUserCmdWithHasher(auth.NewPasswordHasher())
}

// NewUserCmdWithHasher creates a new userCmd instance with a custom PasswordHasher.
func NewUserCmdWithHasher(hasher auth.PasswordHasher) *userCmd {
	if hasher == nil {
		hasher = auth.NewPasswordHasher()
	}
	return &userCmd{
		hasher: hasher,
	}
}

// Command returns the *cli.Command definition for "user".
func (u *userCmd) Command() *cli.Command {
	return &cli.Command{
		Name:  "user",
		Usage: "Manage user accounts",
		Flags: config.UserFlags(),
		Commands: []*cli.Command{
			u.createCommand(),
			u.listCommand(),
			u.disableCommand(),
		},
	}
}

func (u *userCmd) createCommand() *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create a new user account",
		Flags: append([]cli.Flag{
			&cli.StringFlag{
				Name:     "email",
				Usage:    "User email address",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "name",
				Usage:    "User full name",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "password",
				Usage:    "User password",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "avatar-file",
				Usage: "Optional path to avatar image file (PNG, JPEG, WebP <= 2MB)",
			},
		}, config.UserFlags()...),
		Action: u.RunCreate,
	}
}

// RunCreate creates a new user account.
func (u *userCmd) RunCreate(ctx context.Context, cmd *cli.Command) error {
	cfg := config.FromUserContext(ctx, cmd)
	if cfg.DatabaseURL == "" {
		return errors.New("database URL is required (specify --database-url or DATABASE_URL)")
	}

	email := domain.NormalizeEmail(cmd.String("email"))
	name := strings.TrimSpace(cmd.String("name"))
	password := cmd.String("password")
	avatarFilePath := cmd.String("avatar-file")

	if email == "" {
		return errors.New("user email is required")
	}
	if err := domain.ValidateEmail(email); err != nil {
		return fmt.Errorf("invalid email address: %w", err)
	}
	if name == "" {
		return errors.New("user name is required")
	}
	if password == "" {
		return errors.New("user password is required")
	}

	hash, err := u.hasher.Hash(password)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	st, err := store.Open(ctx, "postgres", store.DSNConfig{DSN: cfg.DatabaseURL})
	if err != nil {
		return fmt.Errorf("failed to open database store: %w", err)
	}
	defer st.Close()

	var avatarKey *string
	var stor storage.Storage

	if avatarFilePath != "" {
		fileBytes, err := os.ReadFile(avatarFilePath)
		if err != nil {
			return fmt.Errorf("failed to read avatar file: %w", err)
		}

		if len(fileBytes) > maxAvatarSize {
			return errors.New("avatar file exceeds 2MB limit")
		}

		sniffLen := 512
		if len(fileBytes) < sniffLen {
			sniffLen = len(fileBytes)
		}
		contentType := http.DetectContentType(fileBytes[:sniffLen])
		switch contentType {
		case "image/png", "image/jpeg", "image/webp":
			// valid
		default:
			return fmt.Errorf("invalid avatar file type %q (must be PNG, JPEG, or WebP)", contentType)
		}

		storageDriver := cfg.StorageDriver
		if storageDriver == "" {
			storageDriver = config.DefaultStorageDriver
		}
		stor, err = storage.Open(storageDriver, storage.StorageConfig{
			Driver:  storageDriver,
			DataDir: cfg.DataDir,
		})
		if err != nil {
			return fmt.Errorf("failed to open storage driver: %w", err)
		}

		key, err := storage.NewKey()
		if err != nil {
			return fmt.Errorf("failed to generate storage key: %w", err)
		}

		if err := stor.Put(ctx, key, bytes.NewReader(fileBytes), int64(len(fileBytes)), contentType); err != nil {
			return fmt.Errorf("failed to store avatar: %w", err)
		}
		avatarKey = &key
	}

	user := &domain.User{
		Email:        email,
		Name:         name,
		PasswordHash: &hash,
		AvatarKey:    avatarKey,
	}

	if err := st.Users().Create(ctx, user); err != nil {
		if avatarKey != nil && stor != nil {
			_ = stor.Delete(ctx, *avatarKey)
		}
		return fmt.Errorf("failed to create user: %w", err)
	}

	fmt.Printf("User %s (%s) created successfully (ID: %s)\n", user.Name, user.Email, user.ID)
	return nil
}

func (u *userCmd) listCommand() *cli.Command {
	return &cli.Command{
		Name:   "list",
		Usage:  "List all user accounts",
		Flags:  config.UserFlags(),
		Action: u.RunList,
	}
}

// RunList lists all user accounts.
func (u *userCmd) RunList(ctx context.Context, cmd *cli.Command) error {
	cfg := config.FromUserContext(ctx, cmd)
	if cfg.DatabaseURL == "" {
		return errors.New("database URL is required (specify --database-url or DATABASE_URL)")
	}

	st, err := store.Open(ctx, "postgres", store.DSNConfig{DSN: cfg.DatabaseURL})
	if err != nil {
		return fmt.Errorf("failed to open database store: %w", err)
	}
	defer st.Close()

	users, err := st.Users().List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list users: %w", err)
	}

	if len(users) == 0 {
		fmt.Println("No users found")
		return nil
	}

	fmt.Printf("%-36s  %-30s  %-20s  %s\n", "ID", "EMAIL", "NAME", "STATUS")
	fmt.Println(strings.Repeat("-", 100))
	for _, user := range users {
		status := "active"
		if user.IsDisabled() {
			status = fmt.Sprintf("disabled (%s)", user.DisabledAt.Format(time.RFC3339))
		}
		fmt.Printf("%-36s  %-30s  %-20s  %s\n", user.ID, user.Email, user.Name, status)
	}
	return nil
}

func (u *userCmd) disableCommand() *cli.Command {
	return &cli.Command{
		Name:  "disable",
		Usage: "Disable a user account",
		Flags: append([]cli.Flag{
			&cli.StringFlag{
				Name:     "email",
				Usage:    "Email of user to disable",
				Required: true,
			},
		}, config.UserFlags()...),
		Action: u.RunDisable,
	}
}

// RunDisable disables a user account.
func (u *userCmd) RunDisable(ctx context.Context, cmd *cli.Command) error {
	cfg := config.FromUserContext(ctx, cmd)
	if cfg.DatabaseURL == "" {
		return errors.New("database URL is required (specify --database-url or DATABASE_URL)")
	}

	email := domain.NormalizeEmail(cmd.String("email"))
	if email == "" {
		return errors.New("email is required")
	}

	st, err := store.Open(ctx, "postgres", store.DSNConfig{DSN: cfg.DatabaseURL})
	if err != nil {
		return fmt.Errorf("failed to open database store: %w", err)
	}
	defer st.Close()

	user, err := st.Users().ByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("user %q not found: %w", email, err)
	}

	if user.IsDisabled() {
		fmt.Printf("User %s (%s) is already disabled\n", user.Name, user.Email)
		return nil
	}

	now := time.Now().UTC()
	if err := st.Users().SetDisabled(ctx, user.ID, &now); err != nil {
		return fmt.Errorf("failed to disable user: %w", err)
	}

	fmt.Printf("User %s (%s) disabled successfully\n", user.Name, user.Email)
	return nil
}
