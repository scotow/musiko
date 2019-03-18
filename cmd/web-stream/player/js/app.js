let audio = document.getElementById('audio');

if(Hls.isSupported()) {
    let hls = new Hls();
    hls.loadSource('/playlist.m3u8');
    hls.attachMedia(audio);
    hls.on(Hls.Events.MANIFEST_PARSED,function() {
        audio.play();
    });
} else {
    window.location = '/playlist.m3u8';
}