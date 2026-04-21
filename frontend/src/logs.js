import { writable } from 'svelte/store'

export const logEntries = writable([])

export function addLog(prefix, msg) {
  const ts = new Date().toLocaleTimeString('fr-FR', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  logEntries.update(arr => [...arr, { ts, prefix, msg: String(msg) }])
}

export function clearLogs() {
  logEntries.set([])
}
