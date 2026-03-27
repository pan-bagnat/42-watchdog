package core

import (
	"backend/database"
)

func DatabaseUserToUser(dbUser database.User) User {
	return User{
		ID:            dbUser.ID,
		FtID:          dbUser.FtID,
		FtLogin:       dbUser.FtLogin,
		FtIsStaff:     dbUser.FtIsStaff,
		LastSeen:      dbUser.LastSeen,
		PhotoURL:      dbUser.PhotoURL,
		IsStaff:       dbUser.IsStaff,
		IsBlacklisted: dbUser.IsBlacklisted,
	}
}

func DatabaseUsersToUsers(dbUsers []database.User) (dest []User) {
	for _, user := range dbUsers {
		dest = append(dest, DatabaseUserToUser(user))
	}
	return dest
}
