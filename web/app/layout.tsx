import type { Metadata } from "next";
import { Hanken_Grotesk, Martian_Mono } from "next/font/google";
import { Providers } from "@/lib/providers";
import "./globals.css";

const martian = Martian_Mono({
  subsets: ["latin"],
  variable: "--font-martian",
  weight: ["300", "400", "500", "600", "700"],
});

const hanken = Hanken_Grotesk({
  subsets: ["latin"],
  variable: "--font-hanken",
  weight: ["300", "400", "500", "600", "700"],
});

export const metadata: Metadata = {
  title: "PgFleet — Postgres Control Plane",
  description: "Self-hosted managed Postgres: provisioning, backups, PITR, and analytics.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={`${martian.variable} ${hanken.variable}`}>
      <body>
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
