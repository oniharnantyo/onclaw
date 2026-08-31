package domain

import "errors"

var (
	// ErrNotFound indicates that a requested entity does not exist.
	ErrNotFound = errors.New("not found")

	// ErrConflict indicates a uniqueness or state conflict (e.g. duplicate slug/email).
	ErrConflict = errors.New("conflict")

	// ErrInvalid indicates invalid input or validation failure.
	ErrInvalid = errors.New("invalid request")

	// ErrUnauthenticated indicates missing, invalid, or expired credentials.
	ErrUnauthenticated = errors.New("unauthenticated")

	// ErrForbidden indicates lack of required permissions.
	ErrForbidden = errors.New("forbidden")

	// ErrLastOwnerProtected indicates an action would leave the workspace without an owner.
	ErrLastOwnerProtected = errors.New("cannot demote or remove the last owner")

	// ErrPayloadTooLarge indicates that a request payload exceeds the allowed size limit.
	ErrPayloadTooLarge = errors.New("payload too large")

	// ErrUndecryptable indicates that stored ciphertext could not be decrypted.
	ErrUndecryptable = errors.New("undecryptable")
)
