import { useSearchParams } from 'react-router-dom'

import { Dot, Frame, Pager, Panel, Search, Status, WarriorLink } from '../components'
import { query, useApi } from '../api'
import type { DirectoryWarrior, Page } from '../api'
import { ago, date } from '../format'

export default function Warriors() {
  // The query lives in the URL so a search can be shared and the back button
  // works through one. replace: true while typing, or every keystroke would be
  // its own history entry and going back would replay the word letter by
  // letter.
  const [params, setParams] = useSearchParams()
  const q = params.get('q') ?? ''
  const page = Math.max(1, Number(params.get('page') ?? '1') || 1)

  const set = (next: { q?: string; page?: number }) => {
    const nq = next.q ?? q
    const np = next.q !== undefined ? 1 : (next.page ?? page)

    const out = new URLSearchParams()
    if (nq) out.set('q', nq)
    if (np > 1) out.set('page', String(np))
    setParams(out, { replace: next.q !== undefined })
  }

  const { data, error, loading } = useApi<Page<DirectoryWarrior>>(`/api/warriors${query(q, page)}`)

  return (
    <Frame title="Warriors">
      <Panel
        title="Registered warriors"
        aside={<span>{data ? `${data.total} found` : ''}</span>}
        emblem
        grow
      >
        <Search
          value={q}
          onChange={(v) => set({ q: v })}
          placeholder="Search for part of a name"
        />

        <div style={{ height: 12 }} />

        <Status
          loading={loading && !data}
          error={error}
          empty={!!data && data.items.length === 0}
          emptyText={q ? `Nobody here matches "${q}".` : 'No warriors have registered yet.'}
        />

        {data && data.items.length > 0 && (
          <>
            <div className="tablewrap">
              <table className="list">
                <thead>
                  <tr>
                    <th style={{ width: 34 }} aria-label="Online" />
                    <th>Warrior</th>
                    <th className="num">Tribes</th>
                    <th>Registered</th>
                    <th>Last seen</th>
                  </tr>
                </thead>
                <tbody>
                  {data.items.map((w) => (
                    <tr key={w.guid}>
                      <td>
                        <Dot on={w.online} />
                      </td>
                      <td>
                        <WarriorLink guid={w.guid} name={w.name} tag={w.tag} append={w.append} />
                      </td>
                      <td className="num">{w.tribes}</td>
                      <td>{date(w.created)}</td>
                      <td>{w.online ? 'now' : ago(w.lastSeen)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <Pager
              page={data.page}
              pages={data.pages}
              total={data.total}
              onPage={(n) => set({ page: n })}
            />
          </>
        )}
      </Panel>
    </Frame>
  )
}
