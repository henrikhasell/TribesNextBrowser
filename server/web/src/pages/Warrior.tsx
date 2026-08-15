import { Link, useParams } from 'react-router-dom'

import { Dot, Frame, History, Panel, Prose, Status, Website } from '../components'
import { flag, useApi } from '../api'
import type { WarriorPage } from '../api'
import { date, externalHref, rankName, warriorName } from '../format'

export default function Warrior() {
  const { guid = '' } = useParams()
  const { data, error, loading } = useApi<WarriorPage>(`/api/warriors/${encodeURIComponent(guid)}`)

  const w = data?.warrior
  const title = w ? warriorName(w.name, w.tag, flag(w.append)) : 'Warrior'

  return (
    <Frame title={title}>
      {!w && (
        <Panel title="Warrior">
          <Status loading={loading} error={error} />
        </Panel>
      )}

      {w && (
        <>
          <Panel title="Profile" aside={<Dot on={w.online === 1} />} emblem>
            <div className="row" style={{ marginBottom: 12 }}>
              <h2 className="heading">{title}</h2>
            </div>

            <dl className="fields">
              <dt>Registered</dt>
              <dd>{date(w.creation)}</dd>

              <dt>Web site</dt>
              <dd>
                {w.website ? (
                  <Website href={externalHref(w.website)} text={w.website} />
                ) : (
                  <span className="note">none</span>
                )}
              </dd>

              <dt>Wearing</dt>
              <dd>{w.tag ? w.tag : <span className="note">no tag</span>}</dd>
            </dl>

            {w.info && (
              <>
                <div style={{ height: 12 }} />
                <Prose text={w.info} />
              </>
            )}
          </Panel>

          <div className="columns columns--grow">
            <Panel title={`Tribes (${w.memberships.length})`}>
              {w.memberships.length === 0 ? (
                <p className="empty">Not a member of any tribe.</p>
              ) : (
                <div className="tablewrap">
                  <table className="list" style={{ minWidth: 320 }}>
                    <thead>
                      <tr>
                        <th>Tribe</th>
                        <th>Title</th>
                        <th>Rank</th>
                      </tr>
                    </thead>
                    <tbody>
                      {w.memberships.map((m) => (
                        <tr key={m.id}>
                          <td>
                            <Link to={`/tribes/${m.id}`}>{m.name}</Link>
                          </td>
                          <td className="wrap">{m.title}</td>
                          <td>{rankName(m.rank)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </Panel>

            <Panel title="History">
              <History entries={data.history} />
            </Panel>
          </div>
        </>
      )}
    </Frame>
  )
}
