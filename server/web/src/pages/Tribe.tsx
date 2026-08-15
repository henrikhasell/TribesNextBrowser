import { useParams } from 'react-router-dom'

import { Dot, Frame, History, Panel, Prose, Status, WarriorLink, Website } from '../components'
import { flag, useApi } from '../api'
import type { Member, TribePage } from '../api'
import { date, externalHref, rankName, tribeName } from '../format'

export default function Tribe() {
  const { id = '' } = useParams()
  const { data, error, loading } = useApi<TribePage>(`/api/tribes/${encodeURIComponent(id)}`)

  const t = data?.tribe
  const title = t ? tribeName(t.name, t.tag, flag(t.append)) : 'Tribe'

  return (
    <Frame title={title}>
      {!t && (
        <Panel title="Tribe">
          <Status loading={loading} error={error} />
        </Panel>
      )}

      {t && (
        <>
          <Panel
            title="Profile"
            aside={flag(t.recruiting) ? <span className="badge badge--open">Recruiting</span> : null}
            emblem
          >
            <div className="row" style={{ marginBottom: 12 }}>
              <h2 className="heading">{t.name}</h2>
              <span className="tag">{t.tag}</span>
            </div>

            <dl className="fields">
              <dt>Founded</dt>
              <dd>{date(t.creation)}</dd>

              <dt>Members</dt>
              <dd>{t.members.length}</dd>

              <dt>Web site</dt>
              <dd>
                {t.website ? (
                  <Website href={externalHref(t.website)} text={t.website} />
                ) : (
                  <span className="note">none</span>
                )}
              </dd>
            </dl>

            {t.info && (
              <>
                <div style={{ height: 12 }} />
                <Prose text={t.info} />
              </>
            )}
          </Panel>

          <Panel title={`Roster (${t.members.length})`}>
            {t.members.length === 0 ? (
              <p className="empty">Nobody is in this tribe.</p>
            ) : (
              <div className="tablewrap">
                <table className="list">
                  <thead>
                    <tr>
                      <th style={{ width: 34 }} aria-label="Online" />
                      <th>Warrior</th>
                      <th>Title</th>
                      <th>Rank</th>
                    </tr>
                  </thead>
                  <tbody>
                    {/* Already ordered by rank descending, then name, by
                        ClanView -- the same order the in-game roster uses. */}
                    {t.members.map((m: Member) => (
                      <tr key={m.guid}>
                        <td>
                          <Dot on={m.online === 1} />
                        </td>
                        <td>
                          <WarriorLink
                            guid={m.guid}
                            name={m.name}
                            tag={m.tag}
                            append={flag(m.append)}
                          />
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

          <Panel title="History" grow>
            <History entries={data.history} />
          </Panel>
        </>
      )}
    </Frame>
  )
}
