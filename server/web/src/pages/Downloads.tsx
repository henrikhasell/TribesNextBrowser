import { Frame, Panel, Status } from '../components'
import { useApi } from '../api'
import type { Counts, Release, ReleaseAsset } from '../api'
import { bytes, isoDate } from '../format'

// What each archive is for. The release notes say the same thing, but a
// visitor deciding which button to press should not have to read them.
const WHAT: Record<string, string> = {
  'TNBrowser.vl2':
    'The client package: the community browser, mail and clan screens. This is the one players want.',
  'TNBrowserServer.vl2':
    'The game-server package, which renders clan tags for connecting players. Server operators only.',
}

export default function Downloads() {
  const release = useApi<Release>('/api/releases/latest')
  const stats = useApi<Counts>('/api/stats')

  return (
    <Frame title="TNBrowser">
      <Panel title="The community screens, working again" emblem>
        <p className="prose">
          Tribes 2 shipped with a warrior browser, tribe pages and in-game mail. They talked to
          WON, which shut down in 2003, and have shown an error ever since. TNBrowser points those
          same screens at this server instead &mdash; the shipped screens, not replacements for
          them.
        </p>

        <div className="stats" style={{ marginTop: 14 }}>
          <Stat n={stats.data?.warriors} label="Warriors" />
          <Stat n={stats.data?.tribes} label="Tribes" />
          <Stat n={stats.data?.online} label="Online now" />
        </div>
      </Panel>

      <Panel title="Download" aside={<Version release={release.data} />}>
        <Status loading={release.loading} error={release.error} />

        {release.data?.assets.map((asset) => (
          <Download key={asset.name} asset={asset} />
        ))}

        {release.data?.fallback && <FallbackNote release={release.data} />}
      </Panel>

      <div className="columns">
        <Panel title="Installing">
          <ol className="steps">
            <li>
              You need a copy of Tribes 2 patched by{' '}
              <a href="https://www.tribesnext.com/" target="_blank" rel="noreferrer noopener">
                TribesNext
              </a>
              , which is what the game runs on now, and an account registered with it.
            </li>
            <li>
              Make a mod directory under your install, say <code>GameData/tnb/</code>, and drop{' '}
              <code>TNBrowser.vl2</code> into it.
            </li>
            <li>
              Launch with <code>-mod tnb</code>.
            </li>
            <li>
              The <strong>BROWSER</strong> and <strong>EMAIL</strong> buttons on the launch bar work
              from then on. The backend address is baked into the archive, so there is nothing to
              configure.
            </li>
          </ol>
        </Panel>

        <Panel title="Running a game server">
          <p className="prose">
            <code>TNBrowserServer.vl2</code> is for server operators. Install it the same way on the
            dedicated server and connecting players have their tribe tag rendered into their name,
            the way it worked when WON was up.
          </p>
          <p className="prose" style={{ marginTop: 10 }}>
            A player carries a signed certificate naming their tribe, so the server checks a
            signature rather than making an HTTP request while somebody is connecting. Players do
            not need this package.
          </p>
        </Panel>
      </div>
    </Frame>
  )
}

/**
 * Why the panel is missing its dates and sizes.
 *
 * Two cases, and they are genuinely different. With a tag, the server is naming
 * the release it was configured with and linking straight at it, so the version
 * is right and only the trimmings are missing. Without one, it cannot say which
 * build these links resolve to at all.
 */
function FallbackNote({ release }: { release: Release }) {
  return (
    <p className="note" style={{ marginTop: 12 }}>
      {release.tag ? (
        <>
          GitHub did not answer, so the release date and file sizes are missing. The version above
          is the one this server was configured with and the links point straight at it.
        </>
      ) : (
        <>
          The build could not be identified just now &mdash; GitHub did not answer. These are its
          permanent links to the newest release, so the files are the right ones either way.
        </>
      )}
    </p>
  )
}

function Stat({ n, label }: { n: number | undefined; label: string }) {
  return (
    <div className="stat">
      <span className="stat__n">{n ?? '--'}</span>
      <span className="stat__label">{label}</span>
    </div>
  )
}

/**
 * Which release the buttons below are offering.
 *
 * Not uppercased with the rest of the panel heading: "V1.1.0" is not the name
 * of anything, and a version string is the one label on this page that has to
 * match what is written on the tag character for character.
 */
function Version({ release }: { release: Release | null }) {
  if (!release?.tag) return null

  const when = isoDate(release.published)

  return (
    <span className="version">
      {release.url ? (
        <a href={release.url} target="_blank" rel="noreferrer noopener">
          {release.tag}
        </a>
      ) : (
        release.tag
      )}
      {when && <span className="note"> &mdash; {when}</span>}
    </span>
  )
}

function Download({ asset }: { asset: ReleaseAsset }) {
  const size = bytes(asset.size)
  return (
    <div className="download">
      <div className="download__what">
        <div className="download__name">
          {asset.name}
          {size && <span className="note"> &mdash; {size}</span>}
        </div>
        <p className="download__why">{WHAT[asset.name] ?? 'Part of this release.'}</p>
      </div>
      <a className="btn" href={asset.url} download>
        Download
      </a>
    </div>
  )
}
