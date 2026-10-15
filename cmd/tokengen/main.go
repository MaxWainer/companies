package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/caarlos0/env/v11"

	"example.com/companies/internal/auth/jwt"
	"example.com/companies/internal/platform/config"
)

func main() {
	subject := flag.String("sub", "dev-user", "token subject (the acting user)")
	ttl := flag.Duration("ttl", time.Hour, "token lifetime")
	flag.Parse()

	var cfg config.Auth
	if err := env.ParseWithOptions(&cfg, env.Options{Prefix: config.EnvPrefix + "AUTH_"}); err != nil {
		fail(err)
	}
	if len(cfg.JWTSecret) < config.MinSecretLength {
		fail(fmt.Errorf("%sAUTH_JWT_SECRET must be set to at least %d bytes", config.EnvPrefix, config.MinSecretLength))
	}
	token, err := jwt.Issue(cfg, *subject, *ttl, time.Now())
	if err != nil {
		fail(err)
	}
	fmt.Println(token)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
