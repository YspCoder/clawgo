export function cloneJSON<T>(value: T): T {
  return JSON.parse(JSON.stringify(value));
}
