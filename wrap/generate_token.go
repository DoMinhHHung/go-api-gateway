package main

import (
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"time"
)

func main() {
	secret := []byte("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3ODI5MjM1ODgsInJvbGUiOiJhZG1pbiIsInVzZXJfaWQiOiJVLTk5OTkifQ.EvJezn3TJTWll00H8tfA96uPiGmE46UFa_Gi44BsLx8")
	claims := jwt.MapClaims{
		"user_id": "U-9999",
		"role":    "admin",
		"exp":     time.Now().Add(time.Hour * 1).Unix(), // Hết hạn sau 1 tiếng
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString(secret)

	fmt.Println("Bearer " + tokenString)
}
