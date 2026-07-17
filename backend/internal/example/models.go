package example

import "time"

// User is the example feature's core domain entity.
type User struct {
	ID        string
	Email     string
	CreatedAt time.Time
}

// Profile is a user's profile. It is written atomically with a User to
// demonstrate a transactional repository method (CreateUserWithProfile).
type Profile struct {
	UserID string
	Name   string
}

// EchoRequest is the JSON body accepted by POST /echo.
type EchoRequest struct {
	Message string `json:"message"`
}

// EchoResponse is the JSON body returned by POST /echo.
type EchoResponse struct {
	Echo   string `json:"echo"`
	Length int    `json:"length"`
}

// CreateUserRequest is the JSON body accepted by POST /users.
type CreateUserRequest struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// UserResponse is the wire representation of a User. Keeping a distinct
// response type (rather than serializing the domain type) means storage-only
// fields never leak onto the API by accident.
type UserResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

func newUserResponse(u *User) UserResponse {
	return UserResponse{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt}
}
