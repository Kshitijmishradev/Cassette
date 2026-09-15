import { useEffect, useState } from "react";

export function useHashRoute() {
  const read = () => window.location.hash.slice(1) || "/runs";
  const [route, setRoute] = useState(read);

  useEffect(() => {
    const onHash = () => setRoute(read());
    window.addEventListener("hashchange", onHash);
    if (!window.location.hash) window.location.hash = "/runs";
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  const parts = route.split("/").filter(Boolean).map(decodeURIComponent);
  return { route, parts };
}
