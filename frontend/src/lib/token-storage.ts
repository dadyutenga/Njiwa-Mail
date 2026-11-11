/** Optional demo helper. Client-side storage is accessible to scripts; prefer secure, server-managed sessions in production. */
const KEY = "template-token";
export const tokenStorage = {
  get: () => sessionStorage.getItem(KEY),
  set: (token: string) => sessionStorage.setItem(KEY, token),
  clear: () => sessionStorage.removeItem(KEY),
};
