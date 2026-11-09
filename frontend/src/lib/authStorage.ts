import Cookies from "js-cookie";
import type { User } from "@/types/auth";
import { STORAGE_KEYS } from "./constants";

export const authStorage = {
  getToken: (): string | null => {
    try {
      const cookieToken = Cookies.get(STORAGE_KEYS.auth_token);
      if (cookieToken) return cookieToken;
      return localStorage.getItem(STORAGE_KEYS.auth_token);
    } catch {
      return null;
    }
  },

  setToken: (token: string, remember = true): void => {
    try {
      if (remember) {
        Cookies.set(STORAGE_KEYS.auth_token, token, {
          expires: 7,
          secure: window.location.protocol === "https:",
          sameSite: "lax",
        });
        localStorage.setItem(STORAGE_KEYS.auth_token, token);
      } else {
        sessionStorage.setItem(STORAGE_KEYS.auth_token, token);
      }
    } catch (e) {
      console.error("Failed to persist auth token", e);
    }
  },

  clearToken: (): void => {
    try {
      Cookies.remove(STORAGE_KEYS.auth_token);
      localStorage.removeItem(STORAGE_KEYS.auth_token);
      sessionStorage.removeItem(STORAGE_KEYS.auth_token);
    } catch (e) {
      console.error("Failed to clear auth token", e);
    }
  },

  getUser: (): User | null => {
    try {
      const serialized = localStorage.getItem(STORAGE_KEYS.auth_user);
      if (!serialized) return null;
      return JSON.parse(serialized) as User;
    } catch {
      return null;
    }
  },

  setUser: (user: User): void => {
    try {
      localStorage.setItem(STORAGE_KEYS.auth_user, JSON.stringify(user));
    } catch (e) {
      console.error("Failed to persist user", e);
    }
  },

  clearUser: (): void => {
    try {
      localStorage.removeItem(STORAGE_KEYS.auth_user);
    } catch (e) {
      console.error("Failed to clear user", e);
    }
  },

  clearAll: (): void => {
    authStorage.clearToken();
    authStorage.clearUser();
  },
};
