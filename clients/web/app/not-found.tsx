import { EditorialNavigation } from "@/ui/public/EditorialNavigation";
import { PublicFooter } from "@/ui/public/PublicFooter";

export default function NotFoundPage() {
  return (
    <main className="public-shell">
      <EditorialNavigation />
      <section className="reference-not-found">
        <span>404</span>
        <h1>This record does not exist</h1>
        <p>The address is not part of the Knot public reference.</p>
        <a href="/">Return home</a>
      </section>
      <PublicFooter />
    </main>
  );
}
