import { BrowserRouter, Routes, Route } from "react-router-dom"
import TitlePage from './pages/TitlePage.tsx'
import TimeLinePage from "./pages/TimelinePage.tsx"

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path='/' element={<TitlePage />} />
        <Route path="/timeline" element={<TimeLinePage />} />
      </Routes>
    </BrowserRouter>
  )
}
