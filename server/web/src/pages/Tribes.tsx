import { Link, useSearchParams } from 'react-router-dom'

import { Frame, Pager, Panel, Search, Status } from '../components'
import { query, useApi } from '../api'
import type { DirectoryTribe, Page } from '../api'
import { date, tribeName } from '../format'

export default function Tribes() {
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

  const { data, error, loading } = useApi<Page<DirectoryTribe>>(`/api/tribes${query(q, page)}`)

  return (
    <Frame title="Tribes">
      <Panel
        title="Registered tribes"
        aside={<span>{data ? `${data.total} found` : ''}</span>}
        emblem
        grow
      >
        <Search value={q} onChange={(v) => set({ q: v })} placeholder="Search by name or tag" />

        <div style={{ height: 12 }} />

        <Status
          loading={loading && !data}
          error={error}
          empty={!!data && data.items.length === 0}
          emptyText={q ? `No tribe matches "${q}".` : 'No tribes have been founded yet.'}
        />

        {data && data.items.length > 0 && (
          <>
            <div className="tablewrap">
              <table className="list">
                <thead>
                  <tr>
                    <th>Tribe</th>
                    <th>Tag</th>
                    <th className="num">Members</th>
                    <th className="num">Online</th>
                    <th>Founded</th>
                    <th>Recruiting</th>
                  </tr>
                </thead>
                <tbody>
                  {data.items.map((t) => (
                    <tr key={t.id}>
                      <td>
                        <Link to={`/tribes/${t.id}`}>{tribeName(t.name, t.tag, t.append)}</Link>
                      </td>
                      <td className="tag">{t.tag}</td>
                      <td className="num">{t.members}</td>
                      <td className="num">{t.online}</td>
                      <td>{date(t.created)}</td>
                      <td>
                        {t.recruiting ? <span className="badge badge--open">Open</span> : '--'}
                      </td>
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
