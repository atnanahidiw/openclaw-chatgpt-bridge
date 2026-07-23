(() => {
  // Close the mobile dropdown once a link is taken. Without this an in-page
  // anchor scrolls behind a menu that is still covering the content.
  const mobileMenu = document.querySelector("details.mobile-menu");

  if (mobileMenu) {
    for (const link of mobileMenu.querySelectorAll("a")) {
      link.addEventListener("click", () => {
        mobileMenu.open = false;
      });
    }

    document.addEventListener("click", (event) => {
      if (mobileMenu.open && !mobileMenu.contains(event.target)) {
        mobileMenu.open = false;
      }
    });

    document.addEventListener("keydown", (event) => {
      if (event.key === "Escape") mobileMenu.open = false;
    });
  }

  const useCases = Array.from(document.querySelectorAll("details.use-case"));

  if (useCases.length === 0) return;

  const closeOtherUseCases = (activeUseCase) => {
    for (const useCase of useCases) {
      if (useCase !== activeUseCase) useCase.open = false;
    }
  };

  const openUseCaseFromHash = (hash = window.location.hash) => {
    if (!hash) return;

    const heading = document.getElementById(decodeURIComponent(hash.slice(1)));
    const useCase = heading?.nextElementSibling;

    if (!useCase?.matches("details.use-case")) return;

    closeOtherUseCases(useCase);
    useCase.open = true;

    requestAnimationFrame(() => {
      heading.scrollIntoView({ block: "start" });
    });
  };

  for (const useCase of useCases) {
    useCase.addEventListener("toggle", () => {
      if (useCase.open) closeOtherUseCases(useCase);
    });
  }

  for (const link of document.querySelectorAll('.nav-sublink[href*="#"]')) {
    link.addEventListener("click", () => openUseCaseFromHash(link.hash));
  }

  window.addEventListener("hashchange", () => openUseCaseFromHash());
  openUseCaseFromHash();
})();
