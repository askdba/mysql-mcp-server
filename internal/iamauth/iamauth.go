package iamauth

import (
	"context"
	"database/sql/driver"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"github.com/go-sql-driver/mysql"
)

// Config holds IAM auth parameters for a single connection.
type Config struct {
	Region string // required, e.g. "us-east-1"
}

// NewConnector returns a driver.Connector that generates a fresh RDS IAM auth
// token for every new physical connection opened by the pool. AWS credentials
// are loaded from the standard SDK chain (env vars, ~/.aws/credentials, instance
// profile, ECS task role) at connector-build time; the Credentials value is lazy
// and self-refreshes internally.
//
// mysqlCfg must be fully configured (timeouts, TLS, params) with Passwd cleared;
// BeforeConnect sets Passwd to a fresh token before each physical connect.
func NewConnector(ctx context.Context, mysqlCfg *mysql.Config, cfg Config) (driver.Connector, error) {
	if cfg.Region == "" {
		return nil, fmt.Errorf("iamauth: Region must not be empty")
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("iamauth: failed to load AWS config: %w", err)
	}
	creds := awsCfg.Credentials

	if err := mysqlCfg.Apply(mysql.BeforeConnect(func(ctx context.Context, mc *mysql.Config) error {
		token, err := auth.BuildAuthToken(ctx, mc.Addr, cfg.Region, mc.User, creds)
		if err != nil {
			return fmt.Errorf("iamauth: failed to build auth token: %w", err)
		}
		mc.Passwd = token
		return nil
	})); err != nil {
		return nil, fmt.Errorf("iamauth: failed to apply BeforeConnect: %w", err)
	}
	mysqlCfg.AllowCleartextPasswords = true

	return mysql.NewConnector(mysqlCfg)
}
