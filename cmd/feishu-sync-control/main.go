// feishu-sync-control inspects a rollout or explicitly blocks outbound work.
// It has no resume, purge, source-update or permission-update operation.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/redis/go-redis/v9"
)

func main() {
	action := flag.String("action", "status", "status or stop")
	namespace := flag.String("namespace", "", "approved rollout namespace")
	execute := flag.Bool("execute", false, "explicitly execute stop (never required for status)")
	flag.Parse()
	if *namespace == "" || strings.ContainsAny(*namespace, "{} \t\r\n") || len(*namespace) > 100 ||
		(*action != "status" && *action != "stop") || os.Getenv("REDIS_ADDR") == "" {
		fmt.Fprintln(os.Stderr, "explicit Redis endpoint, valid namespace and action required")
		os.Exit(2)
	}
	if *action == "stop" && !*execute {
		fmt.Fprintln(os.Stderr, "stop requires -execute; no action performed")
		os.Exit(2)
	}
	db, err := strconv.Atoi(os.Getenv("REDIS_DB"))
	if err != nil && os.Getenv("REDIS_DB") != "" {
		os.Exit(2)
	}
	client := redis.NewClient(&redis.Options{
		Addr: os.Getenv("REDIS_ADDR"), DB: db,
		Username: os.Getenv("REDIS_USERNAME"), Password: os.Getenv("REDIS_PASSWORD"),
		MaxRetries: -1, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
	})
	defer func() { _ = client.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if *action == "stop" {
		if err = feishusafety.EmergencyBlock(ctx, client, *namespace); err != nil {
			fmt.Fprintln(os.Stderr, "stop incomplete; verify Redis and controller fencing")
			os.Exit(1)
		}
	}
	prefix := "feishu:safety:{" + *namespace + "}:"
	trip, err := client.Exists(ctx, prefix+"trip").Result()
	if err != nil {
		fmt.Fprintln(os.Stderr, "status unavailable")
		os.Exit(1)
	}
	paused, err := client.Exists(ctx, "asynq:{sync}:paused").Result()
	if err != nil {
		os.Exit(1)
	}
	active, err := client.LLen(ctx, "asynq:{sync}:active").Result()
	if err != nil {
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"action": *action, "trip_latched": trip == 1,
		"sync_paused": paused == 1, "active_queue_entries": active, "execution_fencing": "VERIFY_DATABASE_SEPARATELY",
		"authorization_to_deploy": false,
	})
}
