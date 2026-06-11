import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { World } from "./World";
import "./world.css";

export const metadata: Metadata = {
  title: "the otherworld — overheard",
  description:
    "the live record of the world beside the world, overheard as it settles.",
};

export default async function Page({
  searchParams,
}: {
  searchParams: Promise<{ [key: string]: string | string[] | undefined }>;
}) {
  const params = await searchParams;
  const raw = params.scope ?? "household";
  if (raw !== "household" && raw !== "street") notFound();
  const scope = `scope:${raw}`;

  return (
    <main className="world">
      <header className="furniture micro">
        <span aria-hidden="true">◇</span>
        <span>[ the {raw} ]</span>
        <span>№ live</span>
      </header>

      <World key={scope} scope={scope} />

      <footer className="folio">
        <div className="rule" />
        <div className="folio-row micro">
          <Link href="/">the otherworld</Link>
          {raw === "household" ? (
            <Link href="/world?scope=street">the street →</Link>
          ) : (
            <Link href="/world">← the household</Link>
          )}
        </div>
      </footer>
    </main>
  );
}
