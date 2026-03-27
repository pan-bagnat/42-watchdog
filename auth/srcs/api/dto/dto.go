package api

import (
	"backend/core"
)

func UserToAPIUser(user core.User) User {
	return User{
		ID:            user.ID,
		FtID:          user.FtID,
		FtLogin:       user.FtLogin,
		FtIsStaff:     user.FtIsStaff,
		IsStaff:       user.IsStaff,
		PhotoURL:      user.PhotoURL,
		LastSeen:      user.LastSeen,
		IsBlacklisted: user.IsBlacklisted,
	}
}

func UsersToAPIUsers(users []core.User) (dest []User) {
	for _, user := range users {
		dest = append(dest, UserToAPIUser(user))
	}
	if len(dest) == 0 {
		return (make([]User, 0))
	}
	return dest
}
