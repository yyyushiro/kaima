package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Post struct {
	PostId    uuid.UUID `json:"id"`
	UserId    uuid.UUID `json:"user_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

func addPost(sub uuid.UUID, body string, pool *pgxpool.Pool, ctx context.Context) (*Post, error) {
	var post Post
	err := pool.QueryRow(ctx,
		`INSERT INTO posts (user_id, body) VALUES ($1, $2)
     RETURNING id, user_id, body, created_at`,
		sub, body,
	).Scan(&post.PostId, &post.UserId, &post.Body, &post.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("Inserting post: %w", err)
	}
	return &post, nil
}

func deletePost(userId, postId uuid.UUID, pool *pgxpool.Pool, ctx context.Context) (*Post, error) {
	var post Post
	err := pool.QueryRow(ctx,
		`DELETE FROM posts WHERE id = $1 AND user_id = $2
		RETURNING id, user_id, body, created_at`,
		postId, userId,
	).Scan(&post.PostId, &post.UserId, &post.Body, &post.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("Deleting post: %w", err)
	}
	return &post, nil
}

func getMyPosts(userId uuid.UUID, pool *pgxpool.Pool, ctx context.Context) ([]Post, error) {
	rows, err := pool.Query(ctx, `SELECT id, body FROM posts WHERE user_id = $1`, userId)
	if err != nil {
		return nil, fmt.Errorf("Getting post rows: %w", err)
	}
	defer rows.Close()
	var posts []Post
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.PostId, &p.Body); err != nil {
			return nil, fmt.Errorf("Scanning posts: %w", err)
		}
		posts = append(posts, p)
	}
	// This error block catches whne the iteration finishes abnormally.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("Scanned posts: %w", err)
	}
	return posts, nil
}
