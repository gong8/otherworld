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
          <p className="tagline">
            the world beside the world.
            <br />
            it is already speaking.
          </p>
        </div>
      </section>

      <div className="breath" aria-hidden="true" />

      <section aria-labelledby="overheard-label">
        <Reveal>
          <div className="overheard-label">
            <div className="rule" />
            <span className="micro" id="overheard-label">
              overheard
            </span>
            <div className="rule" />
          </div>
          <Overheard />
        </Reveal>
      </section>

      <div className="breath" aria-hidden="true" />

      <section className="m3">
        <div className="m3-center">
          <Reveal>
            <p className="closing">yours is listening.</p>
          </Reveal>
        </div>
        <footer className="folio">
          <div className="rule" />
          <div className="folio-row micro">
            <span>the otherworld · mmxxvi</span>
            <span>no action is required</span>
          </div>
        </footer>
      </section>
    </main>
  );
}
