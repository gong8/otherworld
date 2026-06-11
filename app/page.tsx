import { Overheard } from "./components/Overheard";
import { Reveal } from "./components/Reveal";

export default function Page() {
  return (
    <main>
      <section className="m1">
        <header className="furniture micro">
          <span aria-hidden="true">◇</span>
          <span>[ A Notice to Residents ]</span>
          <span>№ 0001</span>
        </header>
        <div className="m1-center">
          <h1 className="masthead">the otherworld</h1>
          <p className="tagline">the world beside the world.</p>
        </div>
      </section>

      <div className="breath" aria-hidden="true" />

      <section className="m2" aria-labelledby="terms-label">
        <Reveal>
          <p className="thesis">
            every person has an agent; every thing has one too. the radiator
            has standing, the door can answer, the corner shop can quote terms
            — and the small constant business of living together settles
            itself, on a record anyone affected can read.
          </p>
        </Reveal>
        <Reveal>
          <div className="ruled-label">
            <div className="rule" />
            <span className="micro" id="terms-label">
              the terms
            </span>
            <div className="rule" />
          </div>
          <ol className="terms">
            <li>
              nothing is done in your name beyond a mandate you gave, can
              read, and can revoke.
            </li>
            <li>everything said in your name is yours to read.</li>
            <li>nothing merely commands; nothing merely obeys.</li>
            <li>the door can forget.</li>
            <li>your agent answers to you alone.</li>
            <li>no one may own the air.</li>
          </ol>
        </Reveal>
      </section>

      <div className="breath" aria-hidden="true" />

      <section className="m3" aria-labelledby="overheard-label">
        <div className="m3-center">
          <Reveal>
            <div className="ruled-label">
              <div className="rule" />
              <span className="micro" id="overheard-label">
                overheard
              </span>
              <div className="rule" />
            </div>
            <Overheard />
          </Reveal>
        </div>
        <footer className="folio">
          <div className="rule" />
          <div className="folio-row micro">
            <span>the otherworld · mmxxvi</span>
          </div>
        </footer>
      </section>
    </main>
  );
}
