import React, { useEffect, useRef } from 'react';

export const SpaceParticles: React.FC = () => {
    const canvasRef = useRef<HTMLCanvasElement>(null);

    useEffect(() => {
        const canvas = canvasRef.current;
        if (!canvas) return;
        const ctx = canvas.getContext('2d');
        if (!ctx) return;

        let animationFrameId: number;
        let isDark = typeof document !== 'undefined' ? document.documentElement.classList.contains('theme-dark') : true;
        const particles: Array<{
            x: number;
            y: number;
            size: number;
            speedX: number;
            speedY: number;
            opacity: number;
        }> = [];

        const resize = () => {
            const parent = canvas.parentElement;
            if (parent) {
                canvas.width = parent.clientWidth;
                canvas.height = parent.clientHeight;
            }
        };

        window.addEventListener('resize', resize);
        resize();

        let observer: MutationObserver | null = null;
        if (typeof document !== 'undefined') {
            observer = new MutationObserver(() => {
                isDark = document.documentElement.classList.contains('theme-dark');
            });
            observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });
        }

        // Initialize particles
        const particleCount = Math.floor((canvas.width * canvas.height) / 12000); // Responsive count
        for (let i = 0; i < particleCount; i++) {
            particles.push({
                x: Math.random() * canvas.width,
                y: Math.random() * canvas.height,
                size: Math.random() * 1.5 + 0.5,
                speedX: (Math.random() - 0.5) * 0.4,
                speedY: (Math.random() - 0.5) * 0.4,
                opacity: Math.random() * 0.6 + 0.1,
            });
        }

        const draw = () => {
            ctx.clearRect(0, 0, canvas.width, canvas.height);

            // Draw subtle background gradient
            const gradient = ctx.createRadialGradient(
                canvas.width / 2, canvas.height / 2, 0,
                canvas.width / 2, canvas.height / 2, Math.max(canvas.width, canvas.height)
            );
            if (isDark) {
                gradient.addColorStop(0, 'rgba(255, 133, 82, 0.16)');
                gradient.addColorStop(0.55, 'rgba(112, 41, 22, 0.20)');
                gradient.addColorStop(1, 'rgba(28, 12, 8, 0.88)');
            } else {
                gradient.addColorStop(0, 'rgba(255, 243, 233, 0.30)');
                gradient.addColorStop(0.55, 'rgba(255, 179, 107, 0.16)');
                gradient.addColorStop(1, 'rgba(255, 226, 209, 0.55)');
            }
            ctx.fillStyle = gradient;
            ctx.fillRect(0, 0, canvas.width, canvas.height);

            particles.forEach((p) => {
                p.x += p.speedX;
                p.y += p.speedY;

                if (p.x < 0) p.x = canvas.width;
                if (p.x > canvas.width) p.x = 0;
                if (p.y < 0) p.y = canvas.height;
                if (p.y > canvas.height) p.y = 0;

                ctx.beginPath();
                ctx.arc(p.x, p.y, p.size, 0, Math.PI * 2);
                ctx.fillStyle = isDark
                    ? `rgba(255, 179, 107, ${p.opacity})`
                    : `rgba(217, 72, 28, ${Math.max(0.08, p.opacity * 0.65)})`;
                ctx.fill();
            });

            // Draw connecting lines if close
            ctx.lineWidth = 0.5;
            for (let i = 0; i < particles.length; i++) {
                for (let j = i + 1; j < particles.length; j++) {
                    const dx = particles[i].x - particles[j].x;
                    const dy = particles[i].y - particles[j].y;
                    const dist = Math.sqrt(dx * dx + dy * dy);

                    if (dist < 120) {
                        ctx.beginPath();
                        const lineOpacity = 0.16 * (1 - dist / 120);
                        ctx.strokeStyle = isDark
                            ? `rgba(240, 90, 40, ${lineOpacity})`
                            : `rgba(164, 58, 24, ${lineOpacity * 0.85})`;
                        ctx.moveTo(particles[i].x, particles[i].y);
                        ctx.lineTo(particles[j].x, particles[j].y);
                        ctx.stroke();
                    }
                }
            }

            animationFrameId = requestAnimationFrame(draw);
        };

        draw();

        return () => {
            window.removeEventListener('resize', resize);
            observer?.disconnect();
            cancelAnimationFrame(animationFrameId);
        };
    }, []);

    return <canvas ref={canvasRef} className="absolute inset-0 pointer-events-none z-0 rounded-xl" />;
};
