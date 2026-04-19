import { useEffect, useState } from "react"
import { Link, useNavigate } from "react-router-dom"
import { getMyPosts, type Post } from "../apis/API.ts"

export default function TimeLinePage() {
    const navigate = useNavigate()
    const [posts, setPosts] = useState<Post[]>([])
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState<string | null>(null)

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
                    <ul>
                        {posts.map((post) => (
                            <li key={post.id}>{post.body}</li>
                        ))}
                    </ul>
                ))}
        </div>
    )
}
