package main

import "github.com/zet-plane/live-auction-backend/cmd"

// @title Live Auction Backend API
// @version 1.0
// @description Live auction e-commerce backend API.
// @BasePath /
// @schemes http https
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
func main() {
	cmd.Execute()
}
