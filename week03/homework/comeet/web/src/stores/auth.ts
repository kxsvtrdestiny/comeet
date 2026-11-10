import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import type { Session } from "../services/types";
interface AuthState {
  session: Session | null;
  verified: boolean;
  notice: string;
  verificationError: string;
  setSession: (s: Session) => void;
  clear: (notice?: string) => void;
  verify: (error?: string) => void;
}
export const useAuth = create<AuthState>()(
  persist(
    (set) => ({
      session: null,
      verified: false,
      notice: "",
      verificationError: "",
      setSession: (session) =>
        set({ session, verified: true, notice: "", verificationError: "" }),
      clear: (notice = "") => {
        for (let i = sessionStorage.length - 1; i >= 0; i--) {
          const key = sessionStorage.key(i);
          if (key?.startsWith("comeet.room.")) sessionStorage.removeItem(key);
        }
        set({ session: null, verified: false, notice, verificationError: "" });
      },
      verify: (error = "") =>
        set({ verified: !error, verificationError: error }),
    }),
    {
      name: "comeet.auth.v1",
      storage: createJSONStorage(() => sessionStorage),
      partialize: (state) => ({ session: state.session }),
    },
  ),
);
