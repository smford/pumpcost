/**
 * PumpCost - Tactical Car-Safe Client-Side Fuel Price Tracker
 * Pure Vanilla ES6+ JavaScript (Zero dependencies, zero build step)
 */

(function () {
  'use strict';

  // Application State
  const state = {
    selectedGrade: 'E10',
    currentOutcode: null,
    outcodesIndex: null,
    currentPayload: null,
    userCoords: null,
    stations: []
  };

  // Storage Keys (Declared at top level to prevent temporal dead zone ReferenceErrors)
  const THEME_STORAGE_KEY = 'pumpcost-theme';
  const LEGACY_THEME_STORAGE_KEY = 'pumpcode-theme';
  const IMPORTANT_POSTCODES_KEY = 'pumpcost-important-postcodes';
  const LEGACY_IMPORTANT_POSTCODES_KEY = 'pumpcode-important-postcodes';

  // DOM Elements
  const searchForm = document.getElementById('search-form');
  const searchInput = document.getElementById('outcode-search');
  const searchBtn = document.getElementById('search-btn');
  const autocompleteList = document.getElementById('autocomplete-list');
  const locateBtn = document.getElementById('locate-btn');
  const freshnessLabel = document.getElementById('freshness-label');
  const themeToggle = document.getElementById('theme-toggle');
  const statusContainer = document.getElementById('status-container');
  const resultsHeader = document.getElementById('results-header');
  const resultsTitle = document.getElementById('results-title');
  const resultsCount = document.getElementById('results-count');
  const resultsSummary = document.getElementById('results-summary');
  const stationsGrid = document.getElementById('stations-grid');
  const gradeRadios = document.querySelectorAll('input[name="fuel-grade"]');

  async function init() {
    try {
      setupPWA();
      setupThemeToggle();
      setupGradeSelector();
      setupSearch();
      setupGeolocation();
      setupImportantPostcodes();

      // Load master index
      await loadIndex();

      // Check if user has saved important postcodes for instant 1-tap display
      const savedList = getImportantPostcodes();
      if (savedList && savedList.length > 0) {
        await performSearch(savedList[0].postcode);
      } else {
        // Attempt geolocation with fast initial outcode fallback
        requestGeolocation(false);
      }
    } catch (err) {
      console.error('PumpCost initialization error:', err);
      showStatus('Unable to initialize fuel tracker. Please refresh.', 'alert-danger');
    }
  }

  // --- Progressive Web App (PWA) Setup ---
  function isAppInstalled() {
    try {
      if (typeof window !== 'undefined') {
        if (window.matchMedia && window.matchMedia('(display-mode: standalone)').matches) {
          return true;
        }
        if (window.navigator && window.navigator.standalone === true) {
          return true;
        }
      }
      if (typeof document !== 'undefined' && typeof document.referrer === 'string') {
        if (document.referrer.startsWith('android-app://')) {
          return true;
        }
      }
    } catch (_) {
      // Detection fallback
    }
    return false;
  }

  function setupPWA() {
    const installBtn = document.getElementById('btn-pwa-install');
    const refreshBtn = document.getElementById('btn-refresh-data');
    let deferredPrompt = null;

    function syncButtons() {
      const installed = isAppInstalled();
      if (installed) {
        // When PWA is installed, replace the install button with a refresh button
        if (installBtn) installBtn.hidden = true;
        if (refreshBtn) refreshBtn.hidden = false;
      } else {
        // When running in a browser, keep refresh button available and show install if promptable
        if (refreshBtn) refreshBtn.hidden = false;
        if (installBtn) {
          installBtn.hidden = !deferredPrompt;
        }
      }
    }

    syncButtons();

    // Register Service Worker
    if ('serviceWorker' in navigator) {
      window.addEventListener('load', () => {
        navigator.serviceWorker.register('./sw.js?v=16')
          .then((registration) => {
            registration.update();
            console.log('PumpCost PWA Service Worker registered:', registration.scope);
          })
          .catch((error) => {
            console.warn('PumpCost Service Worker registration failed:', error);
          });
      });
    }

    // Capture install prompt for Chrome/Android
    window.addEventListener('beforeinstallprompt', (e) => {
      if (isAppInstalled()) return;
      e.preventDefault();
      deferredPrompt = e;
      syncButtons();
    });

    if (installBtn) {
      installBtn.addEventListener('click', async () => {
        if (!deferredPrompt) return;
        installBtn.hidden = true;
        deferredPrompt.prompt();
        const choiceResult = await deferredPrompt.userChoice;
        console.log('PWA prompt user choice:', choiceResult.outcome);
        deferredPrompt = null;
        syncButtons();
      });
    }

    window.addEventListener('appinstalled', () => {
      deferredPrompt = null;
      syncButtons();
      console.log('PumpCost PWA installed successfully.');
    });

    if (refreshBtn) {
      refreshBtn.addEventListener('click', async () => {
        await triggerDataRefresh();
      });
    }
  }

  // --- Tactical Refresh Mechanism ---
  async function triggerDataRefresh() {
    const refreshBtn = document.getElementById('btn-refresh-data');
    const icon = refreshBtn ? refreshBtn.querySelector('.refresh-icon') : null;
    if (icon) icon.classList.add('is-spinning');

    try {
      showStatus('Checking for latest fuel price updates...', 'alert-info');
      await loadIndex(true);
      if (state.currentOutcode) {
        await loadOutcode(state.currentOutcode, false, true);
      }
      showStatus('Fuel prices updated', 'alert-info');
      setTimeout(clearStatus, 2500);
    } catch (err) {
      console.warn('Failed to refresh fuel prices:', err);
      showStatus('Unable to refresh prices. Showing cached data.', 'alert-warning');
    } finally {
      if (icon) {
        setTimeout(() => icon.classList.remove('is-spinning'), 600);
      }
    }
  }

  // --- Theme Management ---
  function getStoredTheme() {
    return localStorage.getItem(THEME_STORAGE_KEY) || localStorage.getItem(LEGACY_THEME_STORAGE_KEY);
  }

  function setupThemeToggle() {
    function getEffectiveTheme() {
      const saved = getStoredTheme();
      if (saved === 'dark' || saved === 'light') return saved;
      return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    }

    function applyTheme(theme) {
      if (theme === 'dark') {
        document.documentElement.classList.add('dark');
        document.documentElement.classList.remove('light');
      } else if (theme === 'light') {
        document.documentElement.classList.add('light');
        document.documentElement.classList.remove('dark');
      } else {
        // Auto: adapts automatically to user's device/OS setting
        document.documentElement.classList.remove('dark');
        document.documentElement.classList.remove('light');
      }
      updateThemeUI();
    }

    function updateThemeUI() {
      const saved = getStoredTheme();
      const effective = getEffectiveTheme();
      const modeDesc = saved ? (saved === 'dark' ? 'Dark' : 'Light') : `Auto (${effective === 'dark' ? 'Dark' : 'Light'})`;
      themeToggle.setAttribute('title', `Theme: ${modeDesc} (Click to switch)`);
      themeToggle.setAttribute('aria-label', `Theme: ${modeDesc}`);

      // Sync meta theme-color so PWA status bar / browser chrome matches light/dark surface
      const metaTheme = document.querySelectorAll('meta[name="theme-color"]');
      const barColor = effective === 'dark' ? '#090d16' : '#f8fafc';
      metaTheme.forEach((meta) => {
        meta.setAttribute('content', barColor);
      });
    }

    themeToggle.addEventListener('click', () => {
      const saved = getStoredTheme();
      const isSystemDark = window.matchMedia('(prefers-color-scheme: dark)').matches;

      // Cycle: Auto (matches device) -> Light -> Dark -> Auto
      if (!saved) {
        const next = isSystemDark ? 'light' : 'dark';
        localStorage.setItem(THEME_STORAGE_KEY, next);
        applyTheme(next);
      } else if (saved === 'light') {
        localStorage.setItem(THEME_STORAGE_KEY, 'dark');
        applyTheme('dark');
      } else {
        // Reset to system auto
        localStorage.removeItem(THEME_STORAGE_KEY);
        localStorage.removeItem(LEGACY_THEME_STORAGE_KEY);
        applyTheme(null);
      }
    });

    // Automatically adjust in real time whenever user changes their device system setting
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
      if (!getStoredTheme()) {
        applyTheme(null);
      } else {
        updateThemeUI();
      }
    });

    updateThemeUI();
  }

  // --- Grade Selector (In-Memory Re-sorting) ---
  function setupGradeSelector() {
    gradeRadios.forEach((radio) => {
      radio.addEventListener('change', (e) => {
        state.selectedGrade = e.target.value;
        if (state.stations.length > 0) {
          renderStations();
        }
      });
    });
  }

  // --- Master Index Loader ---
  async function loadIndex(forceReload = false) {
    try {
      const url = forceReload ? `data/outcodes/index.json?_t=${Date.now()}` : 'data/outcodes/index.json';
      const fetchOpts = forceReload ? { cache: 'reload' } : {};
      const res = await fetch(url, fetchOpts);
      if (!res.ok) {
        throw new Error(`Failed to load index: ${res.status}`);
      }
      state.outcodesIndex = await res.json();
      updateFreshnessBadge(state.outcodesIndex.updated_at);
    } catch (err) {
      console.warn('Index load error:', err);
      if (freshnessLabel) freshnessLabel.textContent = 'Data offline';
    }
  }

  // --- Geolocation ---
  function setupGeolocation() {
    locateBtn.addEventListener('click', () => {
      requestGeolocation(true);
    });
  }

  function requestGeolocation(manualTrigger = false) {
    if (!('geolocation' in navigator)) {
      if (manualTrigger) {
        showStatus('Geolocation is not supported by your browser.', 'alert-warning');
      }
      fallbackToFirstOutcode();
      return;
    }

    if (manualTrigger) {
      showStatus('Locking onto GPS coordinates...', 'alert-info');
    } else {
      // Fast initial display: load default outcode immediately so user never sees a blank screen
      if (!state.currentOutcode) {
        fallbackToFirstOutcode();
      }
    }

    navigator.geolocation.getCurrentPosition(
      async (pos) => {
        const { latitude, longitude } = pos.coords;
        state.userCoords = { lat: latitude, lon: longitude };
        await resolveOutcodeFromCoordinates(latitude, longitude);
      },
      (err) => {
        console.warn('Geolocation error:', err.message);
        if (manualTrigger) {
          clearStatus();
          showStatus('Location permission denied. Please enter an outcode or postcode.', 'alert-warning');
        }
        fallbackToFirstOutcode();
      },
      { timeout: 6000, maximumAge: 300000 }
    );
  }

  async function resolveOutcodeFromCoordinates(lat, lon) {
    try {
      const res = await fetch(`https://api.postcodes.io/postcodes?lon=${lon}&lat=${lat}`);
      if (!res.ok) throw new Error('Postcodes.io request failed');
      const data = await res.json();

      if (data.result && data.result.length > 0 && data.result[0].outcode) {
        const detectedOutcode = data.result[0].outcode.toUpperCase();
        const exists = state.outcodesIndex && state.outcodesIndex.outcodes && state.outcodesIndex.outcodes.some((item) => item.outcode === detectedOutcode);
        if (exists) {
          showStatus(`Located near ${detectedOutcode}. Loading live prices...`, 'alert-info');
          await loadOutcode(detectedOutcode);
          return;
        }
      }
      await findAndLoadNearestOutcode(lat, lon, 'your location');
    } catch (err) {
      console.warn('Coordinate resolution failed:', err);
      fallbackToFirstOutcode();
    }
  }

  function fallbackToFirstOutcode() {
    if (state.currentOutcode) return;
    if (state.outcodesIndex && state.outcodesIndex.outcodes.length > 0) {
      loadOutcode(state.outcodesIndex.outcodes[0].outcode);
    }
  }

  // --- Search and Autocomplete ---
  function setupSearch() {
    let debounceTimer;

    if (searchForm) {
      searchForm.addEventListener('submit', (e) => {
        e.preventDefault();
        performSearch();
      });
    }

    if (searchBtn) {
      searchBtn.addEventListener('click', (e) => {
        e.preventDefault();
        performSearch();
      });
    }

    searchInput.addEventListener('input', (e) => {
      clearTimeout(debounceTimer);
      const query = e.target.value.trim().toUpperCase();

      if (!query) {
        closeAutocomplete();
        return;
      }

      debounceTimer = setTimeout(() => {
        handleAutocomplete(query);
      }, 150);
    });

    searchInput.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        closeAutocomplete();
        performSearch();
      } else if (e.key === 'Escape') {
        closeAutocomplete();
      }
    });

    document.addEventListener('click', (e) => {
      if (!searchInput.contains(e.target) && !autocompleteList.contains(e.target)) {
        closeAutocomplete();
      }
    });
  }

  async function performSearch(rawInput) {
    const raw = (rawInput || searchInput.value || '').trim().toUpperCase();
    if (!raw) return;

    closeAutocomplete();
    const candidateOutcode = parseOutcodeFromInput(raw);

    // Direct match against indexed outcodes
    const exists = state.outcodesIndex && state.outcodesIndex.outcodes && state.outcodesIndex.outcodes.some((item) => item.outcode === candidateOutcode);

    if (exists) {
      await loadOutcode(candidateOutcode);
      return;
    }

    // Lookup via postcodes.io for full postcode or nearest outcode
    showStatus(`Looking up postcode "${raw}"...`, 'alert-info');
    const resolved = await resolvePostcodeOrNearest(raw, candidateOutcode);
    if (!resolved) {
      showStatus(`No filling stations found for "${raw}". Try entering a nearby town or district.`, 'alert-warning');
    }
  }

  async function resolvePostcodeOrNearest(query, fallbackOutcode) {
    try {
      const cleanPostcode = encodeURIComponent(query.replace(/[^A-Za-z0-9]/g, ''));
      const res = await fetch(`https://api.postcodes.io/postcodes/${cleanPostcode}`);

      if (res.ok) {
        const data = await res.json();
        if (data.result) {
          const { latitude, longitude, outcode } = data.result;
          state.userCoords = { lat: latitude, lon: longitude };

          const exists = state.outcodesIndex && state.outcodesIndex.outcodes && state.outcodesIndex.outcodes.some((item) => item.outcode === outcode);
          if (exists) {
            await loadOutcode(outcode);
            return true;
          }
          return await findAndLoadNearestOutcode(latitude, longitude, outcode);
        }
      }

      // Try looking up outcode directly
      const outcodeRes = await fetch(`https://api.postcodes.io/outcodes/${encodeURIComponent(fallbackOutcode)}`);
      if (outcodeRes.ok) {
        const outData = await outcodeRes.json();
        if (outData.result && outData.result.latitude) {
          return await findAndLoadNearestOutcode(outData.result.latitude, outData.result.longitude, fallbackOutcode);
        }
      }

      return false;
    } catch (err) {
      console.warn('Postcode lookup error:', err);
      return false;
    }
  }

  async function findAndLoadNearestOutcode(lat, lon, originalDistrict) {
    if (!state.outcodesIndex || !state.outcodesIndex.outcodes || state.outcodesIndex.outcodes.length === 0) {
      return false;
    }

    let nearest = null;
    let minDistance = Infinity;

    for (const item of state.outcodesIndex.outcodes) {
      if (!item.lat || !item.lon) continue;
      const d = calculateDistanceMiles(lat, lon, item.lat, item.lon);
      if (d < minDistance) {
        minDistance = d;
        nearest = item;
      }
    }

    if (!nearest) return false;

    showStatus(
      `No forecourts recorded directly in ${originalDistrict}. Showing nearest stations in ${nearest.outcode} (${minDistance.toFixed(1)} miles away).`,
      'alert-info'
    );
    await loadOutcode(nearest.outcode, true);
    return true;
  }

  function calculateDistanceMiles(lat1, lon1, lat2, lon2) {
    const R = 3958.8; // Earth radius in miles
    const dLat = (lat2 - lat1) * Math.PI / 180;
    const dLon = (lon2 - lon1) * Math.PI / 180;
    const a =
      Math.sin(dLat / 2) * Math.sin(dLat / 2) +
      Math.cos(lat1 * Math.PI / 180) * Math.cos(lat2 * Math.PI / 180) *
      Math.sin(dLon / 2) * Math.sin(dLon / 2);
    const c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
    return R * c;
  }

  function parseOutcodeFromInput(input) {
    const cleaned = input.trim().toUpperCase();
    const parts = cleaned.split(/\s+/);
    if (parts.length >= 2) {
      return parts[0];
    }
    const compact = cleaned.replace(/\s+/g, '');
    if (compact.length > 3) {
      return compact.slice(0, compact.length - 3);
    }
    return compact;
  }

  function handleAutocomplete(query) {
    if (!state.outcodesIndex || !state.outcodesIndex.outcodes) {
      closeAutocomplete();
      return;
    }

    const candidate = parseOutcodeFromInput(query);
    const matches = state.outcodesIndex.outcodes.filter((item) =>
      item.outcode === candidate || item.outcode.startsWith(candidate) || item.outcode.startsWith(query)
    );

    if (matches.length === 0) {
      closeAutocomplete();
      return;
    }

    autocompleteList.innerHTML = '';
    matches.slice(0, 8).forEach((item) => {
      const option = document.createElement('div');
      option.className = 'autocomplete-item';
      option.setAttribute('role', 'option');

      const outcodeSpan = document.createElement('span');
      outcodeSpan.className = 'ac-outcode';
      outcodeSpan.textContent = item.outcode;

      const metaSpan = document.createElement('span');
      metaSpan.className = 'ac-meta';
      const minE10 = item.min_prices && item.min_prices.E10 ? `${item.min_prices.E10.toFixed(1)}p` : '';
      metaSpan.textContent = `${item.station_count} station${item.station_count > 1 ? 's' : ''}${minE10 ? ' | from ' + minE10 : ''}`;

      option.appendChild(outcodeSpan);
      option.appendChild(metaSpan);

      option.addEventListener('click', () => {
        searchInput.value = item.outcode;
        closeAutocomplete();
        loadOutcode(item.outcode);
      });

      autocompleteList.appendChild(option);
    });

    autocompleteList.hidden = false;
  }

  function closeAutocomplete() {
    autocompleteList.hidden = true;
    autocompleteList.innerHTML = '';
  }

  // --- Outcode Data Loader ---
  async function loadOutcode(outcode, keepStatus = false, forceReload = false) {
    if (!outcode) return;
    const cleanOutcode = outcode.toUpperCase();
    state.currentOutcode = cleanOutcode;
    searchInput.value = cleanOutcode;

    if (!keepStatus) {
      showStatus(`Fetching prices for district ${cleanOutcode}...`, 'alert-info');
    }

    try {
      const url = forceReload ? `data/outcodes/${cleanOutcode}.json?_t=${Date.now()}` : `data/outcodes/${cleanOutcode}.json`;
      const fetchOpts = forceReload ? { cache: 'reload' } : {};
      const res = await fetch(url, fetchOpts);
      if (!res.ok) {
        if (res.status === 404) {
          showStatus(`No forecourt records found for district "${cleanOutcode}".`, 'alert-warning');
          stationsGrid.innerHTML = '';
          resultsHeader.hidden = true;
          return;
        }
        throw new Error(`HTTP ${res.status}`);
      }

      const payload = await res.json();
      state.currentPayload = payload;
      let stations = payload.stations || [];

      // Discover adjacent outcodes within ~3.5 miles to include neighboring forecourts
      let includesNearby = false;
      let centerLat = null;
      let centerLon = null;

      if (state.outcodesIndex && state.outcodesIndex.outcodes) {
        const currentEntry = state.outcodesIndex.outcodes.find((item) => item.outcode === cleanOutcode);
        if (currentEntry && currentEntry.lat && currentEntry.lon) {
          centerLat = currentEntry.lat;
          centerLon = currentEntry.lon;
          state.searchCenterCoords = { lat: centerLat, lon: centerLon };

          // Find adjacent outcode districts within 3.5 miles
          const adjacentDistricts = state.outcodesIndex.outcodes
            .filter((item) => item.outcode !== cleanOutcode && item.lat && item.lon)
            .map((item) => ({
              ...item,
              dist: calculateDistanceMiles(centerLat, centerLon, item.lat, item.lon)
            }))
            .filter((item) => item.dist <= 3.5)
            .sort((a, b) => a.dist - b.dist)
            .slice(0, 3);

          if (adjacentDistricts.length > 0) {
            const neighborPromises = adjacentDistricts.map((adj) => {
              const adjUrl = forceReload ? `data/outcodes/${adj.outcode}.json?_t=${Date.now()}` : `data/outcodes/${adj.outcode}.json`;
              return fetch(adjUrl, fetchOpts)
                .then((r) => (r.ok ? r.json() : null))
                .catch(() => null);
            });

            const neighborResults = await Promise.allSettled(neighborPromises);
            const stationMap = new Map();
            stations.forEach((s) => stationMap.set(s.id, s));

            for (const res of neighborResults) {
              if (res.status === 'fulfilled' && res.value && res.value.stations) {
                for (const st of res.value.stations) {
                  if (!stationMap.has(st.id)) {
                    // Check if forecourt is within 4.5 miles of search center
                    if (st.lat && st.lon) {
                      const d = calculateDistanceMiles(centerLat, centerLon, st.lat, st.lon);
                      if (d <= 4.5) {
                        stationMap.set(st.id, st);
                        includesNearby = true;
                      }
                    } else {
                      stationMap.set(st.id, st);
                      includesNearby = true;
                    }
                  }
                }
              }
            }
            stations = Array.from(stationMap.values());
          }
        }
      }

      state.stations = stations;
      state.includesNearby = includesNearby;

      if (!keepStatus) {
        clearStatus();
      }
      updateFreshnessBadge(payload.updated_at);
      renderStations();
    } catch (err) {
      console.error('Failed to load outcode data:', err);
      showStatus(`Could not load price data for "${cleanOutcode}".`, 'alert-warning');
    }
  }

  // --- Rendering Stations (Tactical Car-Safe Layout) ---
  function renderStations() {
    const grade = state.selectedGrade;
    const outcode = state.currentOutcode;

    // In-memory sort: cheapest first for the active grade
    const stationsWithPrices = [...state.stations].sort((a, b) => {
      const priceA = a.prices && a.prices[grade] ? a.prices[grade] : 999999;
      const priceB = b.prices && b.prices[grade] ? b.prices[grade] : 999999;
      return priceA - priceB;
    });

    resultsHeader.hidden = false;
    resultsTitle.textContent = state.includesNearby
      ? `Forecourts in & near ${outcode}`
      : `Forecourts in ${outcode}`;
    resultsCount.textContent = `${stationsWithPrices.length} forecourt${stationsWithPrices.length === 1 ? '' : 's'}`;

    // Update Save District Star button & Active Chip highlight
    updateSaveDistrictButton();
    renderImportantChips();

    // Compute District Statistics Strip (Lowest, Average, Spread)
    if (resultsSummary) {
      resultsSummary.innerHTML = '';
      const pricedOnly = stationsWithPrices.filter((s) => s.prices && s.prices[grade] && s.prices[grade] > 0);

      if (pricedOnly.length > 0) {
        const lowest = pricedOnly[0].prices[grade];
        const highest = pricedOnly[pricedOnly.length - 1].prices[grade];
        const sum = pricedOnly.reduce((acc, s) => acc + s.prices[grade], 0);
        const avg = sum / pricedOnly.length;
        const spread = highest - lowest;

        const lowestPill = document.createElement('span');
        lowestPill.className = 'stat-pill stat-pill-cheapest';
        lowestPill.textContent = `Lowest: ${lowest.toFixed(1)}p`;
        resultsSummary.appendChild(lowestPill);

        const avgPill = document.createElement('span');
        avgPill.className = 'stat-pill';
        avgPill.textContent = `Average: ${avg.toFixed(1)}p`;
        resultsSummary.appendChild(avgPill);

        if (pricedOnly.length > 1 && spread > 0) {
          const spreadPill = document.createElement('span');
          spreadPill.className = 'stat-pill';
          spreadPill.textContent = `Spread: ${spread.toFixed(1)}p`;
          resultsSummary.appendChild(spreadPill);
        }
      }
    }

    stationsGrid.innerHTML = '';

    if (stationsWithPrices.length === 0) {
      stationsGrid.innerHTML = `
        <div class="alert-box alert-warning" style="grid-column: 1 / -1;">
          <span>No forecourts currently recorded in ${outcode}.</span>
        </div>
      `;
      return;
    }

    // Lowest price in active district
    let lowestPrice = Infinity;
    stationsWithPrices.forEach((s) => {
      if (s.prices && s.prices[grade] && s.prices[grade] < lowestPrice) {
        lowestPrice = s.prices[grade];
      }
    });

    stationsWithPrices.forEach((station) => {
      const card = createStationCard(station, grade, lowestPrice);
      stationsGrid.appendChild(card);
    });
  }

  function createStationCard(station, grade, lowestPrice) {
    const card = document.createElement('article');
    card.className = 'forecourt-card';

    const currentPrice = station.prices && station.prices[grade];
    const isCheapest = currentPrice && currentPrice === lowestPrice;

    if (isCheapest) {
      card.classList.add('is-cheapest');
    }

    // 1. Top Section: Identity + Hero Price
    const cardTop = document.createElement('div');
    cardTop.className = 'card-top';

    const identity = document.createElement('div');
    identity.className = 'station-identity';

    const badgesRow = document.createElement('div');
    badgesRow.className = 'station-badges-row';

    const brandName = station.brand || 'Forecourt';
    const brandSlug = brandName.toLowerCase().replace(/[^a-z]/g, '');
    const brandBadge = document.createElement('span');
    brandBadge.className = `retailer-brand-badge brand-${brandSlug}`;
    brandBadge.textContent = brandName;
    badgesRow.appendChild(brandBadge);

    if (isCheapest) {
      const cheapBadge = document.createElement('span');
      cheapBadge.className = 'cheapest-pill-badge';
      cheapBadge.textContent = 'Cheapest';
      badgesRow.appendChild(cheapBadge);
    }

    identity.appendChild(badgesRow);

    const title = document.createElement('h3');
    title.className = 'station-name';
    title.textContent = station.name || brandName;
    identity.appendChild(title);

    const metaRow = document.createElement('div');
    metaRow.className = 'station-meta-row';

    if (station.postcode) {
      const postcode = document.createElement('span');
      postcode.className = 'station-postcode';
      postcode.textContent = station.postcode;
      metaRow.appendChild(postcode);
    }

    const refCoords = state.userCoords || state.searchCenterCoords;
    if (refCoords && station.lat && station.lon) {
      const distMiles = calculateDistanceMiles(refCoords.lat, refCoords.lon, station.lat, station.lon);
      const distBadge = document.createElement('span');
      distBadge.className = 'station-distance';
      distBadge.textContent = `${distMiles.toFixed(1)} mi away`;
      metaRow.appendChild(distBadge);
    }

    identity.appendChild(metaRow);
    cardTop.appendChild(identity);

    // Hero Price Display
    const priceDisplay = document.createElement('div');
    priceDisplay.className = 'price-display';

    if (currentPrice) {
      const priceVal = document.createElement('span');
      priceVal.className = 'price-value';
      priceVal.textContent = `${currentPrice.toFixed(1)}p`;

      const priceUnit = document.createElement('span');
      priceUnit.className = 'price-unit';
      priceUnit.textContent = 'per litre';

      priceDisplay.appendChild(priceVal);
      priceDisplay.appendChild(priceUnit);
    } else {
      const priceNA = document.createElement('span');
      priceNA.className = 'price-na';
      priceNA.textContent = 'N/A';
      priceDisplay.appendChild(priceNA);
    }

    cardTop.appendChild(priceDisplay);
    card.appendChild(cardTop);

    // 2. Alternative Fuel Grades
    if (station.prices) {
      const remainingGrades = Object.keys(station.prices).filter((k) => k !== grade && station.prices[k] > 0);
      if (remainingGrades.length > 0) {
        const altGradesRow = document.createElement('div');
        altGradesRow.className = 'alt-grades-row';

        remainingGrades.forEach((k) => {
          const pill = document.createElement('span');
          pill.className = 'alt-grade-pill';
          pill.innerHTML = `<span class="alt-grade-name">${k}</span> <span class="alt-grade-val">${station.prices[k].toFixed(1)}p</span>`;
          altGradesRow.appendChild(pill);
        });

        card.appendChild(altGradesRow);
      }
    }

    // 3. Forecourt Amenities Badges
    if (station.amenities && station.amenities.length > 0) {
      const amenitiesRow = document.createElement('div');
      amenitiesRow.className = 'amenities-row';

      station.amenities.forEach((amenity) => {
        const chip = document.createElement('span');
        chip.className = 'amenity-tag';
        chip.textContent = formatAmenityName(amenity);
        amenitiesRow.appendChild(chip);
      });

      card.appendChild(amenitiesRow);
    }

    // 4. "Get Directions" Full-Width Action
    const footerAction = document.createElement('div');
    footerAction.className = 'card-actions';

    const directionsLink = document.createElement('a');
    directionsLink.className = 'btn-directions';

    // Human-readable station label for maps
    const stationLabel = [station.brand, station.name, station.postcode]
      .filter(Boolean)
      .join(' ')
      .trim();

    const hasCoords = station.lat != null && station.lon != null && station.lat !== 0;
    const destCoords = hasCoords ? `${station.lat},${station.lon}` : encodeURIComponent(stationLabel);

    const isApple = /iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
    const isAndroid = /Android/i.test(navigator.userAgent);
    const isMobile = isApple || isAndroid;

    // Open in new tab on desktop only; on mobile, open directly so native map app intercepts without dropping parameters
    if (!isMobile) {
      directionsLink.target = '_blank';
      directionsLink.rel = 'noopener noreferrer';
    }

    if (isApple) {
      // Apple Maps driving directions
      const appleLabel = encodeURIComponent(stationLabel);
      directionsLink.href = hasCoords
        ? `https://maps.apple.com/?daddr=${station.lat},${station.lon}&q=${appleLabel}&dirflg=d`
        : `https://maps.apple.com/?daddr=${appleLabel}&dirflg=d`;
    } else if (isAndroid) {
      // Android: Use explicit Android Intent for Google Maps app with daddr
      // Passing daddr coordinates directly ensures navigation routes to the forecourt and NEVER defaults to "Home"
      const webFallback = `https://www.google.com/maps/dir/?api=1&destination=${destCoords}&travelmode=driving`;
      const encodedFallback = encodeURIComponent(webFallback);
      const daddrParam = hasCoords
        ? `${station.lat},${station.lon}(${encodeURIComponent(stationLabel)})`
        : encodeURIComponent(stationLabel);

      directionsLink.href = `intent://maps.google.com/maps?daddr=${daddrParam}&dirflg=d#Intent;scheme=https;package=com.google.android.apps.maps;S.browser_fallback_url=${encodedFallback};end;`;
    } else {
      // Desktop & general web: Universal Google Maps driving directions
      directionsLink.href = `https://www.google.com/maps/dir/?api=1&destination=${destCoords}&travelmode=driving`;
    }

    directionsLink.innerHTML = `
      <span>Get Directions</span>
      <svg class="directions-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <line x1="7" y1="17" x2="17" y2="7"></line>
        <polyline points="7 7 17 7 17 17"></polyline>
      </svg>
    `;

    footerAction.appendChild(directionsLink);
    card.appendChild(footerAction);

    return card;
  }

  function formatAmenityName(raw) {
    const map = {
      car_wash: 'Car Wash',
      air_water: 'Air & Water',
      shop: 'Store',
      pay_at_pump: 'Pay at Pump',
      atm: 'ATM',
      costa_coffee: 'Coffee',
      deli: 'Deli',
      mands_food: 'M&S Food'
    };
    return map[raw] || raw.replace(/_/g, ' ');
  }

  // --- Freshness Badge ---
  function updateFreshnessBadge(isoString) {
    if (!isoString || !freshnessLabel) return;
    const date = new Date(isoString);
    const relative = getRelativeTime(date);
    freshnessLabel.textContent = `Updated ${relative}`;
    freshnessLabel.parentElement.title = `Last synced at: ${date.toLocaleString()}`;
  }

  function getRelativeTime(date) {
    const now = new Date();
    const diffSeconds = Math.floor((now - date) / 1000);

    if (diffSeconds < 60) return 'just now';
    const diffMinutes = Math.floor(diffSeconds / 60);
    if (diffMinutes < 60) return `${diffMinutes}m ago`;
    const diffHours = Math.floor(diffMinutes / 60);
    if (diffHours < 24) return `${diffHours}h ago`;
    const diffDays = Math.floor(diffHours / 24);
    return `${diffDays}d ago`;
  }

  // --- Status Banner Helpers ---
  function showStatus(message, className = 'alert-info') {
    statusContainer.innerHTML = `
      <div class="alert-box ${className}">
        <span>${escapeHTML(message)}</span>
      </div>
    `;
  }

  function clearStatus() {
    statusContainer.innerHTML = '';
  }

  function escapeHTML(str) {
    return str.replace(/[&<>'"]/g, (tag) => ({
      '&': '&amp;',
      '<': '&lt;',
      '>': '&gt;',
      "'": '&#39;',
      '"': '&quot;'
    }[tag] || tag));
  }

  // ==========================================================================
  // Important Postcodes Feature (Persistent 1-Tap Search)
  // ==========================================================================
  function getImportantPostcodes() {
    try {
      const raw = localStorage.getItem(IMPORTANT_POSTCODES_KEY) || localStorage.getItem(LEGACY_IMPORTANT_POSTCODES_KEY);
      if (!raw) return [];
      const parsed = JSON.parse(raw);
      return Array.isArray(parsed) ? parsed : [];
    } catch (_) {
      return [];
    }
  }

  function saveImportantPostcodes(list) {
    try {
      localStorage.setItem(IMPORTANT_POSTCODES_KEY, JSON.stringify(list));
    } catch (_) {}
    renderImportantChips();
    updateSaveDistrictButton();
  }

  function addImportantPostcode(raw) {
    if (!raw) return false;
    const clean = raw.trim().toUpperCase().replace(/[^A-Z0-9 ]/g, '');
    if (!clean) return false;

    const list = getImportantPostcodes();
    if (list.some((item) => item.postcode === clean)) {
      return false;
    }
    list.push({ postcode: clean });
    saveImportantPostcodes(list);
    return true;
  }

  function removeImportantPostcode(raw) {
    if (!raw) return;
    const clean = raw.trim().toUpperCase().replace(/[^A-Z0-9 ]/g, '');
    const list = getImportantPostcodes().filter((item) => item.postcode !== clean);
    saveImportantPostcodes(list);
  }

  function isPostcodeSaved(raw) {
    if (!raw) return false;
    const clean = raw.trim().toUpperCase().replace(/[^A-Z0-9 ]/g, '');
    return getImportantPostcodes().some((item) => item.postcode === clean);
  }

  function renderImportantChips() {
    const container = document.getElementById('important-chips-row');
    if (!container) return;
    container.innerHTML = '';

    const list = getImportantPostcodes();
    if (list.length === 0) {
      const emptyHint = document.createElement('span');
      emptyHint.className = 'chips-empty-hint';
      emptyHint.textContent = 'Save frequent postcodes (Home, Work) for quick 1-tap search';
      container.appendChild(emptyHint);
      return;
    }

    list.forEach((item) => {
      const chip = document.createElement('div');
      chip.className = 'postcode-chip';
      if (state.currentOutcode && (item.postcode === state.currentOutcode || item.postcode.startsWith(state.currentOutcode))) {
        chip.classList.add('is-active');
      }
      chip.setAttribute('role', 'button');
      chip.setAttribute('tabindex', '0');
      chip.setAttribute('aria-label', `Search forecourts for ${item.postcode}`);

      const starIcon = document.createElement('span');
      starIcon.setAttribute('aria-hidden', 'true');
      starIcon.style.color = 'var(--star-gold)';
      starIcon.style.fontSize = '0.75rem';
      starIcon.textContent = '★ ';
      chip.appendChild(starIcon);

      const label = document.createElement('span');
      label.className = 'chip-text';
      label.textContent = item.postcode;
      chip.appendChild(label);

      const removeBtn = document.createElement('button');
      removeBtn.className = 'chip-remove-btn';
      removeBtn.type = 'button';
      removeBtn.innerHTML = '&times;';
      removeBtn.setAttribute('title', `Remove ${item.postcode}`);
      removeBtn.setAttribute('aria-label', `Remove ${item.postcode}`);
      removeBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        removeImportantPostcode(item.postcode);
      });
      chip.appendChild(removeBtn);

      chip.addEventListener('click', () => {
        searchInput.value = item.postcode;
        performSearch(item.postcode);
      });

      chip.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          searchInput.value = item.postcode;
          performSearch(item.postcode);
        }
      });

      container.appendChild(chip);
    });
  }

  function setupImportantPostcodes() {
    const addBtn = document.getElementById('btn-add-postcode-inline');
    const form = document.getElementById('add-postcode-form');
    const input = document.getElementById('add-postcode-input');
    const saveBtn = document.getElementById('btn-save-postcode-submit');
    const cancelBtn = document.getElementById('btn-cancel-postcode-inline');

    if (addBtn && form && input) {
      addBtn.addEventListener('click', () => {
        form.hidden = !form.hidden;
        if (!form.hidden) {
          input.value = '';
          input.focus();
        }
      });

      const handleAddSubmit = () => {
        const val = input.value.trim();
        if (!val) return;
        const added = addImportantPostcode(val);
        form.hidden = true;
        if (added) {
          showStatus(`Saved ${val.toUpperCase()} to Important Postcodes`, 'alert-info');
          performSearch(val);
        }
      };

      if (saveBtn) {
        saveBtn.addEventListener('click', handleAddSubmit);
      }

      input.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          handleAddSubmit();
        } else if (e.key === 'Escape') {
          form.hidden = true;
        }
      });

      if (cancelBtn) {
        cancelBtn.addEventListener('click', () => {
          form.hidden = true;
        });
      }
    }

    renderImportantChips();
    setupSaveDistrictButton();
  }

  function setupSaveDistrictButton() {
    const btn = document.getElementById('btn-save-current-postcode');
    if (!btn) return;
    btn.addEventListener('click', () => {
      const outcode = state.currentOutcode;
      if (!outcode) return;
      if (isPostcodeSaved(outcode)) {
        removeImportantPostcode(outcode);
        showStatus(`Removed ${outcode} from Important Postcodes`, 'alert-info');
      } else {
        addImportantPostcode(outcode);
        showStatus(`Saved ${outcode} to Important Postcodes`, 'alert-info');
      }
      setTimeout(clearStatus, 2500);
    });
  }

  function updateSaveDistrictButton() {
    const btn = document.getElementById('btn-save-current-postcode');
    const label = document.getElementById('save-postcode-text');
    if (!btn || !label) return;

    const outcode = state.currentOutcode;
    if (!outcode) {
      btn.hidden = true;
      return;
    }
    btn.hidden = false;

    const saved = isPostcodeSaved(outcode);
    if (saved) {
      btn.classList.add('is-saved');
      btn.setAttribute('title', `Remove ${outcode} from Important Postcodes`);
      btn.setAttribute('aria-label', `Remove ${outcode} from Important Postcodes`);
      label.textContent = 'Saved';
      const star = btn.querySelector('.star-icon');
      if (star) star.setAttribute('fill', 'currentColor');
    } else {
      btn.classList.remove('is-saved');
      btn.setAttribute('title', `Save ${outcode} to Important Postcodes`);
      btn.setAttribute('aria-label', `Save ${outcode} to Important Postcodes`);
      label.textContent = `Save ${outcode}`;
      const star = btn.querySelector('.star-icon');
      if (star) star.setAttribute('fill', 'none');
    }
  }

  // Initialize once all declarations and handlers are loaded
  init();
})();
