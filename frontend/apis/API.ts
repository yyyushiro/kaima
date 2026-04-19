export type Post = {
    id: string
    body: string
}

export async function getMyPosts(): Promise<Post[]> {
    const response = await fetch("/api/user/me/posts", {
        method: "GET",
        credentials: "include",
    })
    if (!response.ok) {
        throw new Error(`Response status: ${response.status}`)
    }
    const contentType = response.headers.get("content-type")
    if (!contentType || !contentType.includes("application/json")) {
        throw new TypeError("Oops, we haven't got JSON!")
    }
    const data: unknown = await response.json()
    return Array.isArray(data) ? (data as Post[]) : []
}

export async function makePost(body: string): Promise<Post> {
    const response = await fetch("/api/posts", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ body }),
    })
    if (!response.ok) {
        throw new Error(`Response status: ${response.status}`)
    }
    const contentType = response.headers.get("content-type")
    if (!contentType || !contentType.includes("application/json")) {
        throw new TypeError("Oops, we haven't got JSON!")
    }
    return response.json() as Promise<Post>
}
