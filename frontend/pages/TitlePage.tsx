export default function TitlePage() {
    const handleGoogleLogin = () => {
        window.location.href = '/api/auth/google/start'
    }

    return (
        <div>
            <h1>みんなの日記帳</h1>
            <button onClick={handleGoogleLogin}>Continue with Google</button>
        </div>
    )
}