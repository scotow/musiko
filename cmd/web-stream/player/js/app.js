let audio = document.getElementById('audio');
let animation = document.getElementById('animation');
let slider = document.getElementById('slider');

let pause = 'M11,10 L18,13.74 18,22.28 11,26 M18,13.74 L26,18 26,18 18,22.28';
let play = 'M11,10 L17,10 17,26 11,26 M20,10 L26,10 26,26 20,26';

if (Hls.isSupported()) {
    loadCookieVolume();

    let hls = new Hls();
    hls.loadSource('/playlist.m3u8');
    hls.attachMedia(audio);

    hls.on(Hls.Events.MANIFEST_PARSED, function() {
        audio.play();
        updatePlayPauseButton();
    });

    document.body.onkeyup = function(e) {
        if (e.key === ' ') {
            togglePlayPause();
        }
    };

    document.getElementById('play-pause').onclick = togglePlayPause;
    document.getElementById('slider').oninput = volumeChanged;
} else {
    window.location = '/playlist.m3u8';
}

function togglePlayPause() {
    audio.paused ? audio.play() : audio.pause();
    updatePlayPauseButton();
}

function updatePlayPauseButton() {
    animation.setAttribute('from', audio.paused ? play : pause);
    animation.setAttribute('to', audio.paused ? pause : play);
    animation.beginElement();
}

function volumeChanged() {
    audio.volume = this.value / 100;
    document.cookie = 'volume=' + this.value + ';expires=Fri, 31 Dec 9999 23:59:59 GMT;path=/player';
}

function loadCookieVolume() {
    for (let cookie of document.cookie.split(';')) {
        let parts = cookie.split('=');
        if (parts[0] === 'volume') {
            let volume = parseInt(parts[1]);
            slider.value = volume;
            audio.volume = volume / 100;
            return;
        }
    }
}