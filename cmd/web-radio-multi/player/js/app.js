let audio = document.getElementById('audio');
let animation = document.getElementById('animation');
let slider = document.getElementById('slider');

let songName = document.getElementById('song-name');
let songArtist = document.getElementById('song-artist');
let songAlbum = document.getElementById('song-album');

let pause = 'M11,10 L18,13.74 18,22.28 11,26 M18,13.74 L26,18 26,18 18,22.28';
let play = 'M11,10 L17,10 17,26 11,26 M20,10 L26,10 26,26 20,26';

let volumeVariation = 0.15;
let volumeTimeout = null;

let lastHls = null;
let lastSong = null;

fetch('/stations')
    .then(resp => resp.json())
    .then(stations => stationsLoaded(stations));

function stationsLoaded(stations) {
    if (Hls.isSupported()) {
        displayStations(stations);
        loadCookieVolume();

        if (!window.location.hash) {
            window.location.hash = stations[0].name;
        }

        window.onhashchange = loadStationHash;

        document.body.onkeyup = function(e) {
            if (e.key === ' ') togglePlayPause();
        };

        audio.onplay = updatePlayPauseButton;
        audio.onpause = updatePlayPauseButton;
        slider.oninput = volumeSliderChanged;

        document.getElementById('play-pause').onclick = togglePlayPause;
        document.getElementById('volume-down').onclick = volumeDown;
        document.getElementById('volume-up').onclick = volumeUp;

        loadStationHash();
        document.body.classList.remove('loading');
    } else {
        window.location = playlistAddress(stations[0].name);
    }
}

function playlistAddress(station) {
    return '/' + station + '.m3u8';
}

function displayStations(stations) {
    let listElem = document.getElementById('stations');
    for (let station of stations) {
        let stationElem = document.createElement('a');
        stationElem.href = '#' + station.name;
        stationElem.classList.add('station');
        stationElem.innerText = station.display;
        listElem.appendChild(stationElem);
    }
}

function loadStationHash() {
    if (!window.location.hash) return;
    loadStation(window.location.hash.slice(1));
}

function loadStation(station) {
    if (lastHls) {
        lastHls.destroy();
        lastHls = null;
    }

    let hls = new Hls();
    hls.loadSource(playlistAddress(station));
    hls.attachMedia(audio);

    hls.on(Hls.Events.MANIFEST_PARSED, audio.play.bind(audio));
    hls.on(Hls.Events.FRAG_CHANGED, updateInfoIfNeeded);

    lastHls = hls;

    for (let elem of document.querySelectorAll('.stations > .station')) {
        if (elem.href.split('#')[1] === station) {
            elem.classList.add('selected');
        } else {
            elem.classList.remove('selected');
        }
    }
}

function updateInfoIfNeeded(event, data) {
    let song = data.frag.relurl.split('-').slice(0, -1).join('');
    if (song === lastSong) return;

    lastSong = song;
    fetch('/info/' + data.frag.relurl)
        .then(resp => resp.json())
        .then(info => {
            songName.innerText = info.name;
            songArtist.innerText = info.artist;
            songAlbum.innerText = info.album;
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
