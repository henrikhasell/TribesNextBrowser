import { Route, Routes } from 'react-router-dom'

import { Frame, Panel } from './components'
import Downloads from './pages/Downloads'
import Tribe from './pages/Tribe'
import Tribes from './pages/Tribes'
import Warrior from './pages/Warrior'
import Warriors from './pages/Warriors'

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<Downloads />} />
      <Route path="/warriors" element={<Warriors />} />
      <Route path="/warriors/:guid" element={<Warrior />} />
      <Route path="/tribes" element={<Tribes />} />
      <Route path="/tribes/:id" element={<Tribe />} />
      <Route path="*" element={<NotFound />} />
    </Routes>
  )
}

// The server hands index.html to every path it does not recognise, so an
// address nobody ever built arrives here rather than at a 404 page. Saying so
// is this route's whole job.
function NotFound() {
  return (
    <Frame title="Nothing here">
      <Panel title="404" emblem>
        <p className="prose">
          There is no page at that address. The tab bar below goes somewhere real.
        </p>
      </Panel>
    </Frame>
  )
}
