import { useState } from "react";
import { getToken } from "../api/client.ts";
import AuthPage from "../features/auth/AuthPage.tsx";
import AppShell from "./AppShell.tsx";

export default function App() {
  const [token, setToken] = useState<string | null>(getToken());

  if (!token) {
    return <AuthPage onAuthenticated={() => setToken(getToken())} />;
  }
  return <AppShell onLogout={() => setToken(null)} />;
}