import { useEffect, useState } from "react";
import App from "./App.jsx";
import Story from "./Story.jsx";
import Docs from "./Docs.jsx";

export default function Root() {
  const [route, setRoute] = useState(() => window.location.hash.slice(1));
  useEffect(() => {
    const update = () => {
      setRoute(window.location.hash.slice(1));
      window.scrollTo(0, 0);
    };
    window.addEventListener("hashchange", update);
    return () => window.removeEventListener("hashchange", update);
  }, []);
  if (route.startsWith("/docs"))
    return <Docs slug={route.split("/")[2] || ""} />;
  if (route.startsWith("/runs") || route.startsWith("/grid")) return <App />;
  return <Story />;
}
