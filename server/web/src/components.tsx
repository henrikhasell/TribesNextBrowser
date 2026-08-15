// The parts every page is built out of.
//
// One file rather than a directory: there are nine of them, none is longer than
// a screen, and the alternative is nine imports at the top of every page.

import type { ReactNode } from 'react'
import { Link, NavLink } from 'react-router-dom'

import { historyPieces, prosePieces, warriorName } from './format'
import type { HistoryEntry } from './api'

/** The screen: title bar, content, and the game's tab bar along the bottom. */
export function Frame({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="frame">
      <div className="frame__body">
        <div className="titlebar">
          {/* A spacer the width of the close box, so the title is centred
              against the panel and not against what is left of it. */}
          <span style={{ width: 30, flex: 'none' }} aria-hidden="true" />
          <h1 className="titlebar__title">{title}</h1>
          <Link className="titlebar__close" to="/" aria-label="Back to the download page">
            &#10005;
          </Link>
        </div>
        <div className="frame__content">{children}</div>
      </div>
      <TabBar />
    </div>
  )
}

function TabBar() {
  return (
    <nav className="tabbar">
      <Link className="tabbar__brand" to="/">
        TNBrowser
      </Link>
      <Tab to="/" label="Downloads" />
      <Tab to="/warriors" label="Warriors" />
      <Tab to="/tribes" label="Tribes" />
    </nav>
  )
}

function Tab({ to, label }: { to: string; label: string }) {
  return (
    <NavLink
      to={to}
      end={to === '/'}
      className={({ isActive }) => (isActive ? 'tab tab--on' : 'tab')}
    >
      <span className="tab__lamp" />
      {label}
    </NavLink>
  )
}

export function Panel({
  title,
  aside,
  emblem,
  grow,
  children,
}: {
  title?: string
  aside?: ReactNode
  emblem?: boolean
  /** Take the rest of the screen, as the game's list panes do. */
  grow?: boolean
  children: ReactNode
}) {
  const classes = ['panel']
  if (emblem) classes.push('panel--emblem')
  if (grow) classes.push('panel--grow')

  return (
    <section className={classes.join(' ')}>
      {title && (
        <header className="panel__head">
          <span>{title}</span>
          {aside}
        </header>
      )}
      <div className="panel__body">{children}</div>
    </section>
  )
}

/**
 * Text out of a profile, with the game's link markup rendered.
 *
 * pre-wrap, because it was typed into a box that wrapped it and the paragraph
 * breaks are the author's.
 */
export function Prose({ text }: { text: string }) {
  return (
    <p className="prose">
      {prosePieces(text).map((piece, i) =>
        piece.kind === 'web' ? (
          <a key={i} href={piece.href} target="_blank" rel="noreferrer noopener nofollow">
            {piece.text}
          </a>
        ) : (
          <span key={i}>{piece.text}</span>
        ),
      )}
    </p>
  )
}

/** The presence lamp the rosters show beside a name. */
export function Dot({ on }: { on: boolean }) {
  return <span className={on ? 'dot dot--on' : 'dot'} title={on ? 'Online' : 'Offline'} />
}

/** A warrior's name with their tag, linked to their page. */
export function WarriorLink({
  guid,
  name,
  tag,
  append,
}: {
  guid: string
  name: string
  tag?: string
  append?: boolean
}) {
  return <Link to={`/warriors/${encodeURIComponent(guid)}`}>{warriorName(name, tag, append)}</Link>
}

/**
 * The search box.
 *
 * Uncontrolled by the URL on purpose: the page owns the text and pushes it into
 * the query as you type, so a typed search is shareable but the caret never
 * jumps because a route changed underneath it.
 */
export function Search({
  value,
  onChange,
  placeholder,
}: {
  value: string
  onChange: (v: string) => void
  placeholder: string
}) {
  return (
    <div className="search">
      <input
        type="search"
        value={value}
        placeholder={placeholder}
        aria-label={placeholder}
        onChange={(e) => onChange(e.target.value)}
      />
      {value && (
        <button type="button" className="btn btn--small" onClick={() => onChange('')}>
          Clear
        </button>
      )}
    </div>
  )
}

export function Pager({
  page,
  pages,
  total,
  onPage,
}: {
  page: number
  pages: number
  total: number
  onPage: (n: number) => void
}) {
  if (pages <= 1) {
    return <p className="pager">{total} in total</p>
  }
  return (
    <div className="pager">
      <button
        type="button"
        className="btn btn--small"
        disabled={page <= 1}
        onClick={() => onPage(page - 1)}
      >
        Prev
      </button>
      <span>
        Page {page} of {pages} &mdash; {total} in total
      </span>
      <button
        type="button"
        className="btn btn--small"
        disabled={page >= pages}
        onClick={() => onPage(page + 1)}
      >
        Next
      </button>
    </div>
  )
}

/**
 * What a panel shows while it waits, and when it cannot.
 *
 * Returns null once the data is there, so a caller is one line above its own
 * rendering rather than a branch around it.
 */
export function Status({
  loading,
  error,
  empty,
  emptyText,
}: {
  loading: boolean
  error: string | null
  empty?: boolean
  emptyText?: string
}) {
  if (error) return <p className="empty alarm">{error}</p>
  if (loading) return <p className="empty">Loading&hellip;</p>
  if (empty) return <p className="empty">{emptyText ?? 'Nothing here yet.'}</p>
  return null
}

/**
 * The audit trail, with the server's {clan:...} markers turned into links.
 *
 * See historyPieces: an unrecognised marker is left standing rather than
 * dropped, so a kind nobody here knows about is visible instead of invisible.
 */
export function History({ entries }: { entries: HistoryEntry[] }) {
  if (entries.length === 0) return <p className="empty">Nothing has happened yet.</p>

  return (
    <ul className="history">
      {entries.map((e, i) => (
        <li key={`${e.time}-${i}`}>
          <time>{new Date(Number(e.time) * 1000).toISOString().slice(0, 10)}</time>
          <span>
            {historyPieces(e.event).map((piece, j) => {
              if (piece.kind === 'link') {
                return (
                  <Link key={j} to={piece.to}>
                    {piece.text}
                  </Link>
                )
              }
              if (piece.kind === 'web') {
                return piece.href ? (
                  <a key={j} href={piece.href} rel="noreferrer noopener nofollow" target="_blank">
                    {piece.text}
                  </a>
                ) : (
                  <span key={j}>{piece.text}</span>
                )
              }
              return <span key={j}>{piece.text}</span>
            })}
          </span>
        </li>
      ))}
    </ul>
  )
}

/**
 * A web address out of a profile.
 *
 * nofollow and noreferrer because these are player-supplied: this site should
 * neither vouch for where they point nor tell them where the visitor came from.
 * externalHref refuses anything that is not http or https.
 */
export function Website({ href, text }: { href: string; text: string }) {
  if (!href) return <span>{text}</span>
  return (
    <a href={href} target="_blank" rel="noreferrer noopener nofollow">
      {text}
    </a>
  )
}
