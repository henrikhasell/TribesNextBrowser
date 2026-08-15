// The shapes internal/api/site.go answers with, and one hook to fetch them.
//
// Two conventions leak through from the server and are worth knowing about
// while reading the pages. The directory types are the site's own and use real
// booleans and numbers. The profile types (Warrior, Tribe, Member) are the
// game's wire protocol, where every field is a string and a boolean is "1" or
// "0" -- model.go explains why those names cannot be changed.

import { useEffect, useState } from 'react'

export interface Page<T> {
  items: T[]
  total: number
  page: number
  pages: number
}

export interface DirectoryWarrior {
  guid: string
  name: string
  tag: string
  append: boolean
  online: boolean
  tribes: number
  created: number
  lastSeen: number
}

export interface DirectoryTribe {
  id: string
  name: string
  tag: string
  append: boolean
  recruiting: boolean
  members: number
  online: number
  created: number
}

export interface Counts {
  warriors: number
  tribes: number
  online: number
}

/** A tribe as it appears inside a warrior profile. Protocol types: strings. */
export interface Membership {
  id: string
  name: string
  rank: string
  title: string
  tag: string
  append: string
}

export interface Warrior {
  guid: string
  name: string
  tag: string
  append: string
  creation: string
  website: string
  info: string
  online: number
  memberships: Membership[]
}

export interface Member {
  guid: string
  name: string
  tag: string
  append: string
  rank: string
  title: string
  online: number
}

export interface Tribe {
  id: string
  name: string
  tag: string
  append: string
  recruiting: string
  website: string
  info: string
  creation: string
  picture: string
  active: string
  members: Member[]
}

export interface HistoryEntry {
  time: string
  event: string
}

export interface WarriorPage {
  warrior: Warrior
  history: HistoryEntry[]
}

export interface TribePage {
  tribe: Tribe
  history: HistoryEntry[]
}

export interface ReleaseAsset {
  name: string
  url: string
  size: number
}

export interface Release {
  repo: string
  tag?: string
  published?: string
  notes?: string
  url?: string
  assets: ReleaseAsset[]
  fallback: boolean
}

/** "1" is true; anything else is not. The protocol's boolean. */
export const flag = (v: string | undefined): boolean => v === '1'

async function get<T>(path: string, signal: AbortSignal): Promise<T> {
  const resp = await fetch(path, { signal, headers: { Accept: 'application/json' } })
  if (!resp.ok) {
    // Every failure from site.go is {"error": "..."}, written for exactly this.
    const body = await resp.json().catch(() => null)
    throw new Error(body?.error ?? `the server answered ${resp.status}`)
  }
  return resp.json() as Promise<T>
}

export interface Load<T> {
  data: T | null
  error: string | null
  loading: boolean
}

/**
 * Fetch a path and re-fetch when it changes.
 *
 * The abort is what makes typing in a search box safe: every keystroke starts a
 * request, and without cancelling the previous one an early reply can land
 * after a later one and leave the list showing results for a query the box no
 * longer holds.
 */
export function useApi<T>(path: string): Load<T> {
  const [state, setState] = useState<Load<T>>({ data: null, error: null, loading: true })

  useEffect(() => {
    const ctrl = new AbortController()
    setState((s) => ({ data: s.data, error: null, loading: true }))

    get<T>(path, ctrl.signal)
      .then((data) => setState({ data, error: null, loading: false }))
      .catch((err: unknown) => {
        if (ctrl.signal.aborted) return
        setState({ data: null, error: (err as Error).message, loading: false })
      })

    return () => ctrl.abort()
  }, [path])

  return state
}

/** Build a directory query string, leaving out what is at its default. */
export function query(q: string, page: number): string {
  const params = new URLSearchParams()
  if (q.trim()) params.set('q', q.trim())
  if (page > 1) params.set('page', String(page))
  const s = params.toString()
  return s ? `?${s}` : ''
}
