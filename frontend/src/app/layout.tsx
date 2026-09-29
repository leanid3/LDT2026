import type { Metadata } from "next";
import { AuthProvider } from "@/lib/auth";
import "./globals.css";

export const metadata: Metadata = {
  title: "Инспектор ИИ — строительный надзор",
  description: "Проверка строительной документации и протоколы инспектора",
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <html lang="ru"><body><AuthProvider>{children}</AuthProvider></body></html>;
}
