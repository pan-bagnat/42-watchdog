package users

import (
	"encoding/json"
	"net/http"
	"strings"
)

// PostUser creates a new user for your campus.
// @Summary      Create User
// @Description  Imports a new user with the given 42-intranet details and optional role assignments.
// @Tags         Users
// @Accept       json
// @Produce      json
// @Param        input  body      UserPostInput  true  "User creation payload"
// @Success      200    {object}  api.User           "The newly created user"
// @Failure      400    {string}  string             "Invalid JSON input or missing required fields"
// @Failure      500    {string}  string             "Internal server error"
// @Router       /admin/users [post]
func PostUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse and validate input
	var input UserPostInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON input", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(input.FtLogin) == "" || input.FtID <= 0 {
		http.Error(w, "Missing ft_id or ft_login", http.StatusBadRequest)
		return
	}

	// user, err := core.CreateUser(input.FtID, input.FtLogin, input.FtPhoto, input.IsStaff, input.Roles)
	// if err != nil {
	// 	log.Printf("failed to create user: %v", err)
	// 	http.Error(w, "Failed to create user", http.StatusInternalServerError)
	// 	return
	// }

	// // Convert to API model & respond
	// resp := api.UserToAPIUser(*user)
	// if err := json.NewEncoder(w).Encode(resp); err != nil {
	// 	http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	// 	return
	// }
}
