import { useHashRoute } from "./hooks/useHashRoute";
import { useJson } from "./hooks/useJson";
import { Loading, ErrorState } from "./components/Loading";
import { Shell } from "./components/Shell";
import { RunsPage } from "./pages/RunsPage";
import { GridPage } from "./pages/GridPage";
import { WaterfallPage } from "./pages/WaterfallPage";
import { DiffPage } from "./pages/DiffPage";

export default function App() {
  const { route, parts } = useHashRoute();
  const suite = useJson("/api/suite.json");

  if (suite.loading) return <Loading label="Loading cassette index" />;
  if (suite.error)
    return (
      <ErrorState
        title="Could not load the cassette index"
        error={suite.error}
      />
    );

  let page;
  if (parts[0] === "grid") page = <GridPage suite={suite.data} />;
  else if (parts[0] === "runs" && parts[1] && parts[2] === "diff")
    page = (
      <DiffPage
        name={parts[1]}
        summary={suite.data.runs.find((run) => run.name === parts[1])}
      />
    );
  else if (parts[0] === "runs" && parts[1])
    page = <WaterfallPage name={parts[1]} />;
  else page = <RunsPage suite={suite.data} />;

  return (
    <Shell suite={suite.data} route={route}>
      {page}
    </Shell>
  );
}
