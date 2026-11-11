import { Link, useParams } from "react-router-dom";
export default function Detail() {
  const { id } = useParams();
  return (
    <div className="space-y-6">
      <Link to="/records" className="text-sm text-primary">
        ← Back to records
      </Link>
      <h1 className="text-3xl font-bold">Record details</h1>
      <section className="rounded-2xl border bg-card p-6">
        <p className="text-muted-foreground">
          Example detail view for record <strong className="text-foreground">{id}</strong>.
        </p>
        <p className="mt-3 text-sm">Connect this page to your own data source.</p>
      </section>
    </div>
  );
}
