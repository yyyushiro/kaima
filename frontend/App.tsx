import { BrowserRouter, Routes, Route } from "react-router-dom"
import TitlePage from './pages/TitlePage.tsx'

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path='/' element={<TitlePage />} />
      </Routes>
    </BrowserRouter>
  )
}
