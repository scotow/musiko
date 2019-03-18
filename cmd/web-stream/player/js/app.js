let audio = document.getElementById('audio');

if(Hls.isSupported()) {
    let hls = new Hls();
    hls.loadSource('/playlist.m3u8');
    hls.attachMedia(audio);
    hls.on(Hls.Events.MANIFEST_PARSED,function() {
        audio.play();
    });

    document.body.onkeyup = function(e){
        if(e.key === ' ') {
            audio.paused ? audio.play() : audio.pause();
        }
    };
} else {
    window.location = '/playlist.m3u8';
}