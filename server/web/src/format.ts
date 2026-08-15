// Rendering rules shared by every page.

/**
 * How a tag and a name become the thing a player is called.
 *
 * The tag goes in front unless the tribe chose to append it, and the two are
 * concatenated with nothing between them. That is the same rule the game
 * applies -- server.cs:689 for the name on a scoreboard, webstuff.cs:12 for the
 * browser's own link text -- and it is why CreateClan on the server keeps the
 * whitespace around a tag instead of trimming it: the separator, if a tribe
 * wants one, is part of the tag.
 *
 * So do not trim here either. "[TC] " and "[TC]" are different tags, chosen
 * deliberately, and a tribe that typed the space is looking at it in the
 * preview while they type.
 */
export function warriorName(name: string, tag?: string, append?: boolean): string {
  if (!tag) return name
  return append ? name + tag : tag + name
}

/** The same, for a tribe's own name in a heading. */
export function tribeName(name: string, tag?: string, append?: boolean): string {
  return warriorName(name, tag, append)
}

/**
 * Unix seconds as a date. The game shows "Registered: 2026-08-10" and nothing
 * finer, so neither does this.
 */
export function date(seconds: number | string | undefined): string {
  const n = typeof seconds === 'string' ? Number(seconds) : seconds
  if (!n || !Number.isFinite(n)) return '--'
  return new Date(n * 1000).toISOString().slice(0, 10)
}

/** An ISO timestamp from GitHub as the same kind of date. */
export function isoDate(iso: string | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : d.toISOString().slice(0, 10)
}

/** How long ago, in the roughest terms that are still useful. */
export function ago(seconds: number | undefined): string {
  if (!seconds) return 'never'
  const secs = Math.max(0, Math.floor(Date.now() / 1000) - seconds)
  if (secs < 300) return 'now'
  if (secs < 3600) return `${Math.floor(secs / 60)}m ago`
  if (secs < 86400) return `${Math.floor(secs / 3600)}h ago`
  const days = Math.floor(secs / 86400)
  if (days < 365) return `${days}d ago`
  return `${Math.floor(days / 365)}y ago`
}

export function bytes(n: number | undefined): string {
  if (!n) return ''
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

/** The five ranks, by the numbers the wire protocol uses. */
const RANKS = ['Recruit', 'Member', 'Officer', 'Senior Admin', 'Leader'] as const

export function rankName(rank: string | number): string {
  const n = typeof rank === 'string' ? Number(rank) : rank
  return RANKS[n] ?? 'Member'
}

/**
 * A history line, which arrives as display text carrying the mod's own markup.
 *
 * The server writes {clan:Name} and {warrior:Name} markers (store.Ref) and the
 * proxy expands them into Torque <a:...> links for the game. Nothing expanded
 * them for a browser, so this does it: the pieces come back as text and links,
 * and anything unrecognised is left standing exactly as written -- the same
 * rule dbproxy.linkRefs follows, and for the same reason. Silently dropping a
 * marker would hide the fact that something wrote one nobody knows about.
 */
export type HistoryPiece =
  | { kind: 'text'; text: string }
  | { kind: 'link'; text: string; to: string }
  | { kind: 'web'; text: string; href: string }

export function historyPieces(event: string): HistoryPiece[] {
  const out: HistoryPiece[] = []
  const re = /\{(clan|warrior|web):([^}]*)\}/g

  let last = 0
  for (let m = re.exec(event); m !== null; m = re.exec(event)) {
    if (m.index > last) out.push({ kind: 'text', text: event.slice(last, m.index) })

    const [, kind = '', name = ''] = m
    if (kind === 'web') {
      out.push({ kind: 'web', text: name, href: externalHref(name) })
    } else if (kind === 'clan') {
      // A marker names a tribe rather than identifying one, so the link is a
      // search for that name. The alternative would be the server writing ids
      // into history lines that the game renders verbatim.
      out.push({ kind: 'link', text: name, to: `/tribes?q=${encodeURIComponent(name)}` })
    } else {
      out.push({ kind: 'link', text: name, to: `/warriors?q=${encodeURIComponent(name)}` })
    }
    last = m.index + m[0].length
  }
  if (last < event.length) out.push({ kind: 'text', text: event.slice(last) })
  return out
}

/**
 * Profile text, which is written for the game and carries the game's markup.
 *
 * A player types their description into a GuiMLTextCtrl, so what comes back can
 * hold Torque's link syntax -- <a:www.example.com>text</a> -- which renders as a
 * link in the game and as literal angle brackets anywhere else. This turns that
 * one form into a real link.
 *
 * Only that one form. Anything else Torque understands is left exactly as
 * written, for the reason dbproxy.linkRefs gives about unknown markers:
 * silently swallowing markup nobody here knows about hides the fact that it is
 * there. A visitor seeing a stray tag can report it; a visitor seeing nothing
 * cannot.
 */
export function prosePieces(text: string): HistoryPiece[] {
  const out: HistoryPiece[] = []
  const re = /<a:([^>]*)>([\s\S]*?)<\/a>/g

  let last = 0
  for (let m = re.exec(text); m !== null; m = re.exec(text)) {
    if (m.index > last) out.push({ kind: 'text', text: text.slice(last, m.index) })

    const [, target = '', label = ''] = m
    const href = externalHref(target)
    // A target that is not a web address is one of the game's own verbs
    // (acceptinvite, tribe, warrior), which mean nothing outside it. Show the
    // label as plain text rather than pretend it is somewhere to go.
    out.push(href ? { kind: 'web', text: label, href } : { kind: 'text', text: label })
    last = m.index + m[0].length
  }
  if (last < text.length) out.push({ kind: 'text', text: text.slice(last) })
  return out
}

/**
 * A player-supplied web address as something safe to put in href.
 *
 * Profiles hold whatever was typed -- "www.tribesnext.com" as often as a full
 * URL -- so a scheme is added when there is none. Anything that is not http or
 * https is refused outright rather than rendered: a javascript: address in a
 * profile field is the one way this site could be turned against a visitor.
 */
export function externalHref(site: string): string {
  const trimmed = site.trim()
  if (!trimmed) return ''
  const withScheme = /^[a-z][a-z0-9+.-]*:/i.test(trimmed) ? trimmed : `https://${trimmed}`
  try {
    const url = new URL(withScheme)
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.href : ''
  } catch {
    return ''
  }
}
