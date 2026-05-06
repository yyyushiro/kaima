package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Post struct {
	PostId    uuid.UUID `json:"id"`
	UserId    uuid.UUID `json:"user_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	LikedByMe bool      `json:"liked_by_me"`
}

type Like struct {
	LikerId   uuid.UUID `json:"liker_id"`
	PostId    uuid.UUID `json:"post_id"`
	CreatedAt time.Time `json:"created_at"`
}

func AddPost(sub uuid.UUID, body string, pool *pgxpool.Pool, ctx context.Context) (*Post, error) {
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

func DeletePost(userId, postId uuid.UUID, pool *pgxpool.Pool, ctx context.Context) (*Post, error) {
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

func GetMyPosts(userId uuid.UUID, pool *pgxpool.Pool, ctx context.Context) ([]Post, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, body,
			EXISTS (
				SELECT 1 FROM likes
				WHERE likes.user_id = $1 AND likes.post_id = posts.id
			)
		FROM posts WHERE user_id = $1`, userId)
	if err != nil {
		return nil, fmt.Errorf("Getting post rows: %w", err)
	}
	defer rows.Close()
	var posts []Post
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.PostId, &p.Body, &p.LikedByMe); err != nil {
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

// LikePost increments likes with the check of no duplicates.
func LikePost(likerId, postId uuid.UUID, pool *pgxpool.Pool, ctx context.Context) (*Like, error) {
	// I'm not sure if I have to validate likerid and postid. I will do them later if needed.
	var l Like
	err := pool.QueryRow(ctx, `SELECT user_id, post_id, created_at FROM likes WHERE user_id = $1 AND post_id = $2`, likerId, postId).Scan(&l.LikerId, &l.PostId, &l.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			err := pool.QueryRow(ctx, `INSERT INTO likes (user_id, post_id) VALUES ($1, $2) RETURNING user_id, post_id, created_at`, likerId, postId).Scan(&l.LikerId, &l.PostId, &l.CreatedAt)
			if err != nil {
				return nil, fmt.Errorf("Scanned like: %w", err)
			}
			return &l, nil
		}
		return nil, fmt.Errorf("Scanned like: %w", err)
	}
	return &l, fmt.Errorf("User %d already liked post %d", l.LikerId, l.PostId)
}

// UndoLikePost undos the likes.
func UndoLikePost(likerId, postId uuid.UUID, pool *pgxpool.Pool, ctx context.Context) (*Like, error) {
	var l Like
	err := pool.QueryRow(ctx, `SELECT user_id, post_id, created_at FROM likes WHERE user_id = $1 AND post_id = $2`, likerId, postId).Scan(&l.LikerId, &l.PostId, &l.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("Scanned like: %w", err)
	}

	err = pool.QueryRow(ctx, `DELETE FROM likes WHERE user_id = $1 AND post_id = $2 RETURNING user_id, post_id, created_at`, likerId, postId).Scan(&l.LikerId, &l.PostId, &l.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("Deleted a like: %w", err)
	}

	return &l, nil
}
