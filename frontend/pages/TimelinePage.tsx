import { useEffect, useState } from "react"
import { Link, useNavigate } from "react-router-dom"
import { getMyPosts, likePost, unlikePost, type Post } from "../apis/API.ts"

export default function TimeLinePage() {
    const navigate = useNavigate()
    const [posts, setPosts] = useState<Post[]>([])
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState<string | null>(null)
    const [pendingLikePostId, setPendingLikePostId] = useState<string | null>(
        null,
    )
    const [likeToggleError, setLikeToggleError] = useState<string | null>(null)

    useEffect(() => {
        let cancelled = false
        setLoading(true)
        setError(null)
        getMyPosts()
            .then((data) => {
                if (!cancelled) setPosts(data)
            })
            .catch((err: unknown) => {
                if (cancelled) return
                const message =
                    err instanceof Error ? err.message : "Failed to load posts"
                const isUnauthorized =
                    err instanceof Error &&
                    err.message.includes("Response status: 401")
                setError(
                    isUnauthorized ? "Not signed in." : message,
                )
            })
            .finally(() => {
                if (!cancelled) setLoading(false)
            })
        return () => {
            cancelled = true
        }
    }, [])

    async function toggleLike(post: Post) {
        if (pendingLikePostId !== null) return
        setLikeToggleError(null)
        setPendingLikePostId(post.id)
        try {
            if (post.liked_by_me) {
                await unlikePost(post.id)
            } else {
                await likePost(post.id)
            }
            setPosts((prev) =>
                prev.map((p) =>
                    p.id === post.id
                        ? { ...p, liked_by_me: !p.liked_by_me }
                        : p,
                ),
            )
        } catch (err: unknown) {
            const message =
                err instanceof Error ? err.message : "Could not update like"
            setLikeToggleError(message)
        } finally {
            setPendingLikePostId(null)
        }
    }

    return (
        <div>
            <h1>This is the timeline.</h1>
            <p>
                <button type="button" onClick={() => navigate("/post")}>
                    New post
                </button>
            </p>
            {loading && <p>Loading…</p>}
            {error && (
                <>
                    <p role="alert">{error}</p>
                    {error === "Not signed in." && (
                        <p>
                            <Link to="/">Sign in</Link>. If login still fails,
                            set Google OAuth redirect and{" "}
                            <code>REDIRECT_URI</code> to{" "}
                            <code>
                                http://localhost:5173/api/auth/callback/google
                            </code>{" "}
                            so cookies are stored for this dev server.
                        </p>
                    )}
                </>
            )}
            {!loading && !error &&
                (posts.length === 0 ? (
                    <p>No posts yet.</p>
                ) : (
                    <>
                        {likeToggleError && (
                            <p role="alert">{likeToggleError}</p>
                        )}
                        <ul>
                            {posts.map((post) => {
                                const busy = pendingLikePostId === post.id
                                const liked = post.liked_by_me
                                return (
                                    <li key={post.id}>
                                        <span>{post.body}</span>{" "}
                                        <button
                                            type="button"
                                            disabled={busy}
                                            aria-pressed={liked}
                                            aria-label={
                                                liked ? "Unlike" : "Like"
                                            }
                                            onClick={() => toggleLike(post)}
                                            style={{
                                                color: liked
                                                    ? "#c62828"
                                                    : "inherit",
                                                background: liked
                                                    ? "rgba(198, 40, 40, 0.12)"
                                                    : undefined,
                                                border: "1px solid",
                                                borderColor: liked
                                                    ? "#c62828"
                                                    : "#ccc",
                                                borderRadius: 4,
                                                cursor: busy
                                                    ? "wait"
                                                    : "pointer",
                                                lineHeight: 1.2,
                                                padding: "2px 8px",
                                            }}
                                        >
                                            ♥
                                        </button>
                                    </li>
                                )
                            })}
                        </ul>
                    </>
                ))}
        </div>
    )
}
