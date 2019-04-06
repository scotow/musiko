let audio = document.getElementById('audio');
let animation = document.getElementById('animation');
let slider = document.getElementById('slider');

let trackName = document.getElementById('track-name');
let trackArtist = document.getElementById('track-artist');
let trackAlbum = document.getElementById('track-album');

let pause = 'M11,10 L18,13.74 18,22.28 11,26 M18,13.74 L26,18 26,18 18,22.28';
let play = 'M11,10 L17,10 17,26 11,26 M20,10 L26,10 26,26 20,26';

let volumeVariation = 0.15;
let volumeTimeout = null;

let currentStation = null;
let currentHls = null;
let currentTrack = null;

fetch('/stations')
    .then(resp => resp.json())
    .then(stations => stationsLoaded(stations));

function stationsLoaded(stations) {
    if (Hls.isSupported()) {
        displayStations(stations);
        loadCookieVolume();

        document.body.onkeyup = function(e) {
            if (e.key === ' ') togglePlayPause();
        };

        audio.onplay = updatePlayPauseButton;
        audio.onpause = updatePlayPauseButton;
        slider.oninput = volumeSliderChanged;

        document.getElementById('play-pause').onclick = togglePlayPause;
        document.getElementById('volume-down').onclick = volumeDown;
        document.getElementById('volume-up').onclick = volumeUp;

        let currentName = uriStationName();
        let station = stations[0];
        if (currentName) {
            for (let s of stations) {
                if (s.name === currentName) {
                    station = s;
                    break;
                }
            }
        }
        switchToStation(station);
        document.body.classList.remove('loading');
    } else {
        window.location = playlistAddress(stations[0].name);
    }
}

function playlistAddress(station) {
    return `/stations/${station}/playlist.m3u8`;
}

function displayStations(stations) {
    let listElem = document.getElementById('stations');
    for (let station of stations) {
        let elem = document.createElement('div');
        elem.classList.add('station');
        elem.innerText = station.display;
        elem.onclick = switchToStation.bind(null, station);
        listElem.appendChild(elem);

        station.elem = elem;
    }
}

function uriStationName() {
    let parts = window.location.pathname.split('/');
    if (parts.length < 3) {
        return null;
    }
    if (parts[1] !== 'player' || !parts[2]) {
        return null;
    }

    return parts[2];
}

function switchToStation(station) {
    if (currentStation) {
        currentStation.elem.classList.remove('selected');
        currentStation = null;
    }

    station.elem.classList.add('selected');
    document.title = `Musiko | ${station.display}`;
    history.replaceState(null, null, `/player/${station.name}`);

    loadStation(station);
    currentStation = station;
}

function loadStation(station) {
    if (currentHls) {
        currentHls.destroy();
        currentHls = null;
    }

    let hls = new Hls();
    hls.loadSource(playlistAddress(station.name));
    hls.attachMedia(audio);

    hls.on(Hls.Events.MANIFEST_PARSED, audio.play.bind(audio));
    hls.on(Hls.Events.FRAG_CHANGED, updateInfoIfNeeded);

    currentHls = hls;
}

function updateInfoIfNeeded(event, data) {
    let track = data.frag.relurl.split('/').slice(1, 5).join('/');
    if (track === currentTrack) return;

    currentTrack = track;
    fetch(`/${track}/info`)
        .then(resp => resp.json())
        .then(info => {
            trackName.innerText = info.name;
            trackArtist.innerText = info.artist;
            trackAlbum.innerText = info.album;
        });
}

function togglePlayPause() {
    audio.paused ? audio.play() : audio.pause();
}

function updatePlayPauseButton() {
    animation.setAttribute('from', audio.paused ? play : pause);
    animation.setAttribute('to', audio.paused ? pause : play);
    animation.beginElement();
}

function volumeDown() {
    audio.volume = Math.max(0, audio.volume - volumeVariation);
    updateVolumeSlider();
    saveCookieVolume();
}

function volumeUp() {
    audio.volume = Math.min(1, audio.volume + volumeVariation);
    updateVolumeSlider();
    saveCookieVolume();
}

function volumeSliderChanged() {
    audio.volume = slider.value / 100;
    saveCookieVolume();
}

function saveCookieVolume() {
    if (volumeTimeout) clearTimeout(volumeTimeout);

    volumeTimeout = setTimeout(function() {
        document.cookie = 'volume=' + (audio.volume * 100) + ';expires=Fri, 31 Dec 9999 23:59:59 GMT;path=/player';
        volumeTimeout = null;
    }, 250);
}

function updateVolumeSlider() {
    slider.value = audio.volume * 100;
}

function loadCookieVolume() {
    for (let cookie of document.cookie.split(';')) {
        let parts = cookie.split('=');
        if (parts[0] === 'volume') {
            let volume = parseInt(parts[1]);
            audio.volume = volume / 100;
            updateVolumeSlider();
            return;
        }
    }
}
