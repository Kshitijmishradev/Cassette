import { useEffect, useState } from "react";

export function useJson(path) {
  const [state, setState] = useState({
    data: null,
    error: null,
    loading: true,
  });

  useEffect(() => {
    if (!path) {
      setState({ data: null, error: null, loading: false });
      return undefined;
    }
    const controller = new AbortController();
    setState({ data: null, error: null, loading: true });
    fetch(path, { signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) {
          const error = new Error(`Request failed (${response.status})`);
          error.status = response.status;
          throw error;
        }
        const body = await response.text();
        try {
          return JSON.parse(body);
        } catch (cause) {
          const error = new Error(
            body.trimStart().startsWith("<")
              ? "Resource not found"
              : "Response was not valid JSON",
            { cause },
          );
          if (body.trimStart().startsWith("<")) error.status = 404;
          throw error;
        }
      })
      .then((data) => setState({ data, error: null, loading: false }))
      .catch((error) => {
        if (error.name !== "AbortError")
          setState({ data: null, error, loading: false });
      });
    return () => controller.abort();
  }, [path]);

  return state;
}
